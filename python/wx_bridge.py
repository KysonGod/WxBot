#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import json
import os
import queue
import sys
import threading
import time
import traceback
import uuid

wx = None
wechat_ctor = None
wechat_backend = ""
keep_running_thread = None
stop_event = threading.Event()

out_queue = queue.Queue(maxsize=4096)
msg_store = {}
msg_store_lock = threading.Lock()

WECHAT_BACKENDS = ["wxautox", "wxauto", "wxautox_wechatbot"]


def now_ts():
    return int(time.time())


def load_wechat_ctor(backend_name):
    if backend_name == "wxautox":
        from wxautox import WeChat  # type: ignore
        return WeChat

    if backend_name == "wxauto":
        from wxauto import WeChat  # type: ignore
        return WeChat

    if backend_name == "wxautox_wechatbot":
        os.environ.setdefault("PROJECT_NAME", "iwyxdxl/WeChatBot_WXAUTO_SE")
        from wxautox_wechatbot import WeChat  # type: ignore
        try:
            from wxautox_wechatbot.param import WxParam  # type: ignore
            WxParam.ENABLE_FILE_LOGGER = False
            WxParam.FORCE_MESSAGE_XBIAS = True
        except Exception:
            pass
        return WeChat

    raise ValueError(f"unknown backend: {backend_name}")


def to_primitive(value):
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, dict):
        return {str(k): to_primitive(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [to_primitive(v) for v in value]
    return str(value)


def enqueue_message(payload):
    try:
        out_queue.put_nowait(payload)
    except queue.Full:
        try:
            out_queue.get_nowait()
        except Exception:
            pass
        try:
            out_queue.put_nowait(payload)
        except Exception:
            pass


def send_response(req_id, ok, result=None, error=None):
    envelope = {"type": "response", "id": str(req_id), "ok": bool(ok)}
    if ok:
        envelope["result"] = to_primitive(result)
    else:
        envelope["error"] = str(error) if error else "unknown error"
    enqueue_message(envelope)


def send_event(event_name, data):
    enqueue_message({"type": "event", "event": event_name, "data": to_primitive(data)})


def writer_loop():
    while not stop_event.is_set() or not out_queue.empty():
        try:
            item = out_queue.get(timeout=0.2)
        except queue.Empty:
            continue
        try:
            line = json.dumps(item, ensure_ascii=False)
            sys.stdout.write(line + "\n")
            sys.stdout.flush()
        except Exception:
            traceback.print_exc(file=sys.stderr)


def store_message(event_id, msg):
    with msg_store_lock:
        msg_store[event_id] = (msg, time.time())


def get_message(event_id):
    with msg_store_lock:
        item = msg_store.get(event_id)
    if item is None:
        return None
    return item[0]


def cleanup_loop():
    ttl_seconds = 15 * 60
    while not stop_event.is_set():
        time.sleep(30)
        cutoff = time.time() - ttl_seconds
        with msg_store_lock:
            stale_ids = [k for k, (_, ts) in msg_store.items() if ts < cutoff]
            for sid in stale_ids:
                msg_store.pop(sid, None)


def message_listener(msg, chat):
    try:
        event_id = uuid.uuid4().hex
        store_message(event_id, msg)

        who = getattr(chat, "who", None)
        if not who and hasattr(chat, "ChatInfo"):
            try:
                info = chat.ChatInfo()
                who = info.get("who")
            except Exception:
                who = None

        data = {
            "event_id": event_id,
            "who": str(who or ""),
            "sender": str(getattr(msg, "sender", "") or ""),
            "msg_type": str(getattr(msg, "type", "") or ""),
            "attr": str(getattr(msg, "attr", "") or ""),
            "content": str(getattr(msg, "content", None) or getattr(msg, "text", "") or ""),
            "timestamp": now_ts(),
        }
        send_event("message", data)
    except Exception:
        traceback.print_exc(file=sys.stderr)


def ensure_wx_initialized(show):
    global wx, wechat_ctor, wechat_backend
    if wx is None:
        backend_errors = []
        for backend in WECHAT_BACKENDS:
            try:
                ctor = load_wechat_ctor(backend)
                wx = ctor()
                wechat_ctor = ctor
                wechat_backend = backend
                break
            except BaseException as exc:
                backend_errors.append(f"{backend}: {exc}")
                if not isinstance(exc, ModuleNotFoundError):
                    traceback.print_exc(file=sys.stderr)
                wx = None
                wechat_ctor = None
                wechat_backend = ""
        if wx is None:
            raise RuntimeError("init WeChat failed: " + " | ".join(backend_errors))
    if show and hasattr(wx, "Show"):
        try:
            wx.Show()
        except Exception:
            traceback.print_exc(file=sys.stderr)
    return wx


def ensure_keep_running_thread():
    global keep_running_thread
    if keep_running_thread is not None and keep_running_thread.is_alive():
        return

    def _target():
        try:
            if wx is not None and hasattr(wx, "KeepRunning"):
                wx.KeepRunning()
        except Exception:
            traceback.print_exc(file=sys.stderr)

    keep_running_thread = threading.Thread(target=_target, name="wx-keep-running", daemon=True)
    keep_running_thread.start()


def handle_message_action(params):
    event_id = str(params.get("event_id", "")).strip()
    action = str(params.get("action", "")).strip()
    option = params.get("option")
    if not event_id:
        raise ValueError("event_id is required")
    if not action:
        raise ValueError("action is required")

    msg = get_message(event_id)
    if msg is None:
        raise ValueError("message not found or expired")

    if action == "download":
        return msg.download()
    if action == "capture":
        return msg.capture()
    if action == "to_text":
        return msg.to_text()
    if action == "get_url":
        return msg.get_url()
    if action == "get_messages":
        return msg.get_messages()
    if action == "quote_content":
        return getattr(msg, "quote_content", None)
    if action == "tickle":
        msg.tickle()
        return True
    if action == "select_option":
        if option is None:
            raise ValueError("option is required for select_option")
        msg.select_option(str(option))
        return True

    raise ValueError(f"unsupported message action: {action}")


def dispatch(method, params):
    global wx

    if method == "ping":
        return {"pong": True}

    if method == "init":
        show = bool((params or {}).get("show", True))
        ensure_wx_initialized(show)
        ensure_keep_running_thread()
        return {"nickname": str(getattr(wx, "nickname", "") or "")}

    if method == "add_listen_chat":
        ensure_wx_initialized(False)
        nickname = str((params or {}).get("nickname", "")).strip()
        if not nickname:
            raise ValueError("nickname is required")
        ok = wx.AddListenChat(nickname=nickname, callback=message_listener)
        if not ok:
            raise RuntimeError(f"AddListenChat failed for {nickname}")
        return True

    if method == "send_msg":
        ensure_wx_initialized(False)
        who = str((params or {}).get("who", "")).strip()
        msg = str((params or {}).get("msg", ""))
        if not who:
            raise ValueError("who is required")
        if not wx.SendMsg(msg=msg, who=who):
            raise RuntimeError("SendMsg returned false")
        return True

    if method == "send_files":
        ensure_wx_initialized(False)
        who = str((params or {}).get("who", "")).strip()
        filepath = str((params or {}).get("filepath", "")).strip()
        if not who or not filepath:
            raise ValueError("who and filepath are required")
        if not wx.SendFiles(filepath=filepath, who=who):
            raise RuntimeError("SendFiles returned false")
        return True

    if method == "voice_call":
        ensure_wx_initialized(False)
        who = str((params or {}).get("who", "")).strip()
        if not who:
            raise ValueError("who is required")
        wx.VoiceCall(who)
        return True

    if method == "message_action":
        ensure_wx_initialized(False)
        return handle_message_action(params or {})

    if method == "shutdown":
        stop_event.set()
        return True

    raise ValueError(f"unknown method: {method}")


def request_loop():
    for raw_line in sys.stdin:
        line = raw_line.strip()
        if not line:
            continue
        if line.startswith("\ufeff"):
            line = line.lstrip("\ufeff")

        req_id = ""
        try:
            req = json.loads(line)
            req_id = req.get("id", "")
            method = req.get("method", "")
            params = req.get("params", {})
            result = dispatch(method, params)
            send_response(req_id, True, result=result)
        except BaseException as exc:
            traceback.print_exc(file=sys.stderr)
            send_response(req_id, False, error=str(exc))

    stop_event.set()


def main():
    writer = threading.Thread(target=writer_loop, name="bridge-writer", daemon=True)
    writer.start()

    cleaner = threading.Thread(target=cleanup_loop, name="msg-cleaner", daemon=True)
    cleaner.start()

    request_loop()
    writer.join(timeout=1.5)


if __name__ == "__main__":
    main()
