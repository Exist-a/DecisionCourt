package courtroom

import (
	"context"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/model"
)

// rebuttalRepoAsSink 把本包 RebuttalRepository 适配成 agent.RebuttalSink (narrow contract).
// agent.RebuttalSink 只需要 Insert(link) 方法. RebuttalRepository.Insert(link) 返回 row + err,
// 而 RebuttalSink.Insert 也返回 (row, err). 两个签名完全一致, 适配是直接转发.
//
// 这是 PR-4 的窄桥, 让 Service.WithRebuttalRepository 同时 wire 到:
//   - orchestrator.SetRebuttalRepository (read-side, agent narrow)
//   - orchestrator.SetRebuttalSink (write-side, agent narrow)
// 而 caller 只用传 RebuttalRepository (broad).
func rebuttalRepoAsSink(r RebuttalRepository) agent.RebuttalSink {
	if r == nil {
		return nil
	}
	return rebuttalSinkAdapter{repo: r}
}

type rebuttalSinkAdapter struct {
	repo RebuttalRepository
}

func (a rebuttalSinkAdapter) Insert(ctx context.Context, link model.EvidenceRebuttalLink) (model.EvidenceRebuttalLink, error) {
	return a.repo.Insert(ctx, link)
}