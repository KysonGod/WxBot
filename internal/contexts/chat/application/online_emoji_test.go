package application

import "testing"

func TestParseOnlineDecision_JSONTrue(t *testing.T) {
	raw := `{"need_online": true, "query": "今天北京天气"}`
	ok, query := parseOnlineDecision(raw, "fallback")
	if !ok {
		t.Fatalf("expected need_online=true")
	}
	if query != "今天北京天气" {
		t.Fatalf("unexpected query: %q", query)
	}
}

func TestParseOnlineDecision_JSONFalse(t *testing.T) {
	raw := `{"need_online": false, "query": ""}`
	ok, query := parseOnlineDecision(raw, "fallback")
	if ok {
		t.Fatalf("expected need_online=false, got query=%q", query)
	}
}

func TestParseOnlineDecision_TextFallback(t *testing.T) {
	raw := "需要联网\n今天上海股价走势"
	ok, query := parseOnlineDecision(raw, "fallback")
	if !ok {
		t.Fatalf("expected need_online=true")
	}
	if query != "今天上海股价走势" {
		t.Fatalf("unexpected query: %q", query)
	}
}

func TestParseOnlineDecision_TextNoNeed(t *testing.T) {
	raw := "不需要联网"
	ok, _ := parseOnlineDecision(raw, "fallback")
	if ok {
		t.Fatalf("expected need_online=false")
	}
}
