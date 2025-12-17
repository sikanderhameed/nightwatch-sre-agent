package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// SlackPayload defines the structure of the message sent to Slack
type SlackPayload struct {
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment defines rich message content
type Attachment struct {
	Color  string  `json:"color,omitempty"`
	Title  string  `json:"title,omitempty"`
	Text   string  `json:"text,omitempty"`
	Fields []Field `json:"fields,omitempty"`
	Footer string  `json:"footer,omitempty"`
	Ts     int64   `json:"ts,omitempty"`
}

// Field defines a key-value pair in an attachment
type Field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// SendAlert sends a rich formatted alert to Slack using a webhook
func SendAlert(title string, diagnosis string, command string, success bool) error {
	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	if webhookURL == "" {
		// Log warning but don't error hard if no webhook is set (local dev)
		fmt.Println("Warning: SLACK_WEBHOOK_URL not set, skipping notification")
		return nil
	}

	color := "#FF0000" // Red for failure
	statusIcon := "❌"
	if success {
		color = "#36a64f" // Green for success
		statusIcon = "✅"
	}

	payload := SlackPayload{
		Attachments: []Attachment{
			{
				Color: color,
				Title: fmt.Sprintf("%s %s", statusIcon, title),
				Text:  diagnosis,
				Fields: []Field{
					{
						Title: "Remediation Command",
						Value: fmt.Sprintf("`%s`", command),
						Short: false,
					},
					{
						Title: "Status",
						Value: getStatusText(success),
						Short: true,
					},
				},
				Footer: "Nightwatch AI SRE Agent",
				Ts:     time.Now().Unix(),
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to send slack request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned status: %s", resp.Status)
	}

	return nil
}

func getStatusText(success bool) string {
	if success {
		return "Remediated Successfully"
	}
	return "Remediation Failed / Human Intervention Required"
}
