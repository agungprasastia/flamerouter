package models

import (
	"flamerouter/internal/tokenrefresh"
)

// RegisterDefaultResolvers wires Copilot, Kiro, Grok-CLI, Kimchi, ClinePass, and Qoder resolvers into Engine.
func (e *Engine) RegisterDefaultResolvers() {
	rm := tokenrefresh.NewRefreshManager()

	copilot := &CopilotResolver{
		RefreshManager: rm,
		Client:         nil,
	}
	e.Register("github", copilot)
	e.Register("copilot", copilot)

	kiro := &KiroResolver{
		RefreshManager: rm,
		Client:         nil,
	}
	e.Register("kiro", kiro)

	grokCli := &GrokCliResolver{
		RefreshManager: rm,
		Client:         nil,
	}
	e.Register("grok-cli", grokCli)

	kimchi := &KimchiResolver{
		Client: nil,
	}
	e.Register("kimchi", kimchi)

	clinepass := &ClinePassResolver{
		Client: nil,
	}
	e.Register("clinepass", clinepass)

	qoder := &QoderResolver{
		Client: nil,
	}
	e.Register("qoder", qoder)
}
