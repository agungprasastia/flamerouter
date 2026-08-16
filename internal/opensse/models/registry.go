package models

import (
	"flamerouter/internal/tokenrefresh"
)

// RegisterDefaultResolvers wires Copilot, Kiro, Grok-CLI, Kimchi, ClinePass, and Qoder resolvers into Engine.
func (e *Engine) RegisterDefaultResolvers() {
	rm := tokenrefresh.NewRefreshManager()

	copilot := &CopilotResolver{RefreshManager: rm}
	e.Register("github", copilot)
	e.Register("copilot", copilot)

	kiro := &KiroResolver{RefreshManager: rm}
	e.Register("kiro", kiro)

	grokCli := &GrokCliResolver{RefreshManager: rm}
	e.Register("grok-cli", grokCli)

	kimchi := &KimchiResolver{}
	e.Register("kimchi", kimchi)

	clinepass := &ClinePassResolver{}
	e.Register("clinepass", clinepass)

	qoder := &QoderResolver{}
	e.Register("qoder", qoder)
}
