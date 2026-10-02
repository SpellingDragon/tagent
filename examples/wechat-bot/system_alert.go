// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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
		return
	}

	text := fmt.Sprintf("[System Alert] Detected an unprocessed failure record for the previous generation's restart script (run/SYSTEM_ALERT):\n%s",
		string(data))
	ta.InjectMessageWithSource("system_alert", model.Message{
		Role:    model.RoleUser,
		Content: text,
	})
	log.Infof("[system-alert] staged failure injected via system_alert source (%d bytes)", len(data))

	if err := os.Rename(alertPath, alertPath+".consumed"); err != nil {
		log.Warnf("[system-alert] consume-marker rename failed: %v", err)
	} else {
		log.Infof("[system-alert] SYSTEM_ALERT renamed -> consumed marker")
	}
}
