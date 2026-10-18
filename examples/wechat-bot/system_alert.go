// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// alertInjector is the narrow inbox face the startup alert is queued into.
type alertInjector interface {
	InjectInternal(addr event.Address, source string, msg trpcmodel.Message)
}

// maybeConsumeSystemAlert is the one-shot startup hook: wait for the runtime to
// settle, queue any pending restart failure for the first user turn, mark
// consumed. Every step is logged; nothing here may crash the bot.
func maybeConsumeSystemAlert(inj alertInjector, runDir string, delay time.Duration) {
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
	inj.InjectInternal(event.Address{}, "system_alert", trpcmodel.Message{
		Role:    trpcmodel.RoleUser,
		Content: text,
	})
	log.Infof("[system-alert] staged failure queued via system_alert source (%d bytes)", len(data))

	if err := os.Rename(alertPath, alertPath+".consumed"); err != nil {
		log.Warnf("[system-alert] consume-marker rename failed: %v", err)
	} else {
		log.Infof("[system-alert] SYSTEM_ALERT renamed -> consumed marker")
	}
}
