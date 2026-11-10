package quarantine

import (
	"github.com/blackbeardONE/CELLS/internal/alerting"
	"github.com/blackbeardONE/CELLS/internal/logging"
)

func TriggerQuarantine(logger *logging.Logger, message string) {
	logger.Warn("Quarantine triggered:", message)
	alerting.Alert(message)
	// alerting.AlertQuarantineTriggered() // Remove or implement this function if needed
}
