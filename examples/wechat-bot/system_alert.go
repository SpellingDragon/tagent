package main

import (
	"fmt"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"os"
	"path/filepath"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// System-alert dead-man switch (openspec 5.8): the restart/insurance scripts
// stage run/SYSTEM_ALERT when a restart attempt FAILs (EXIT/TERM trap) — the
// 2026-09-14 03:52/12:05 incidents showed 13 consecutive FAILs leaving the
// agent completely blind because the verdict only landed in restart.log.
// On boot we consume the marker the same way REINCARNATION_NOTICE is
// consumed: detect -> inject via the meditation source -> rename (consume).
//
// Residual gap (v1): an alert staged while the CURRENT process keeps running
// is only seen at next boot; in-process hot-reload failures are covered by
// EmitSystemAlert (agent/inject.go) instead.

// maybeConsumeSystemAlert is the one-shot startup hook: wait for the event
// loop to settle, inject any pending restart failure, mark consumed. Every
// step is logged; nothing here may crash the bot.
func maybeConsumeSystemAlert(ta *agent.TagentAgent, runDir string, delay time.Duration) {
	if !filepath.IsAbs(runDir) {
		if exe, err := os.Executable(); err == nil {
			runDir = filepath.Join(filepath.Dir(exe), runDir)
		}
	}
	alertPath := filepath.Join(runDir, "SYSTEM_ALERT")

	time.Sleep(delay)

	data, err := os.ReadFile(alertPath)
	if err != nil {
		return // no pending alert: silent no-op
	}

	text := fmt.Sprintf("[System Alert] Detected an unprocessed failure record for the previous generation's restart script (run/SYSTEM_ALERT):\n%s",
		string(data))
	ta.InjectMessageWithSource("meditation", model.Message{
		Role:    model.RoleUser,
		Content: text,
	})
	log.Infof("[system-alert] staged failure injected via meditation source (%d bytes)", len(data))

	if err := os.Rename(alertPath, alertPath+".consumed"); err != nil {
		log.Warnf("[system-alert] consume-marker rename failed: %v", err)
	} else {
		log.Infof("[system-alert] SYSTEM_ALERT renamed -> consumed marker")
	}
}
