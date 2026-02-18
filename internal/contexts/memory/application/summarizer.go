package application

import "context"

type Summarizer struct {
	repo Repository
}

func NewSummarizer(repo Repository) *Summarizer {
	return &Summarizer{repo: repo}
}

func (s *Summarizer) Run(ctx context.Context) {
	_ = ctx
	// 占位：后续将接入记忆总结策略。
}
