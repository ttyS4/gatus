package matrix

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/TwiN/gatus/v4/alerting/alert"
	"github.com/TwiN/gatus/v4/client"
	"github.com/TwiN/gatus/v4/core"
)

// AlertProvider is the configuration necessary for sending an alert using Matrix
type AlertProvider struct {
	// HomeserverURL is the custom homeserver to use (optional)
	HomeserverURL string `yaml:"homeserver-url"`
	// AccessToken is the bot user's access token to send messages
	AccessToken string `yaml:"access-token"`
	// InternalRoomID is the room that the bot user has permissions to send messages to
	InternalRoomID string `yaml:"internal-room-id"`

	// DefaultAlert is the default alert configuration to use for endpoints with an alert of the appropriate type
	DefaultAlert *alert.Alert `yaml:"default-alert,omitempty"`

	// Overrides is a list of Override that may be prioritized over the default configuration
	Overrides []Override `yaml:"overrides,omitempty"`
}

// Override is a case under which the default integration is overridden
type Override struct {
	Group string `yaml:"group"`

	HomeserverURL  string `yaml:"homeserver-url"`
	AccessToken    string `yaml:"access-token"`
	InternalRoomID string `yaml:"internal-room-id"`
}

const defaultHomeserverURL = "https://matrix-client.matrix.org"

type matrixProviderConfig struct {
	HomeserverURL  string `yaml:"homeserver-url"`
	AccessToken    string `yaml:"access-token"`
	InternalRoomID string `yaml:"internal-room-id"`
}

// IsValid returns whether the provider's configuration is valid
func (provider *AlertProvider) IsValid() bool {
	registeredGroups := make(map[string]bool)
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if isAlreadyRegistered := registeredGroups[override.Group]; isAlreadyRegistered || override.Group == "" || len(override.AccessToken) == 0 || len(override.InternalRoomID) == 0 {
				return false
			}
			registeredGroups[override.Group] = true
		}
	}
	return len(provider.AccessToken) > 0 && len(provider.InternalRoomID) > 0
}

// Send an alert using the provider
func (provider *AlertProvider) Send(endpoint *core.Endpoint, alert *alert.Alert, result *core.Result, resolved bool) error {
	buffer := bytes.NewBuffer([]byte(provider.buildRequestBody(endpoint, alert, result, resolved)))
	config := provider.getConfigForGroup(endpoint.Group)
	if config.HomeserverURL == "" {
		config.HomeserverURL = defaultHomeserverURL
	}
	txnId := randStringBytes(24)
	request, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("%s/_matrix/client/r0/rooms/%s/send/m.room.message/%s?access_token=%s",
			config.HomeserverURL,
			url.PathEscape(config.InternalRoomID),
			txnId,
			url.QueryEscape(config.AccessToken),
		),
		buffer,
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.GetHTTPClient(nil).Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode > 399 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("call to provider alert returned status code %d: %s", response.StatusCode, string(body))
	}
	return err
}

// buildRequestBody builds the request body for the provider
func (provider *AlertProvider) buildRequestBody(endpoint *core.Endpoint, alert *alert.Alert, result *core.Result, resolved bool) string {
	return fmt.Sprintf(`{
	"msgtype": "m.text",
	"format": "org.matrix.custom.html",
	"body": "%s",
	"formatted_body": "%s"
}`,
		buildPlaintextMessageBody(endpoint, alert, result, resolved),
		buildHTMLMessageBody(endpoint, alert, result, resolved),
	)
}

// buildPlaintextMessageBody builds the message body in plaintext to include in request
func buildPlaintextMessageBody(endpoint *core.Endpoint, alert *alert.Alert, result *core.Result, resolved bool) string {
	var message, results string
	if resolved {
		message = fmt.Sprintf("An alert for `%s` has been resolved after passing successfully %d time(s) in a row", endpoint.DisplayName(), alert.SuccessThreshold)
	} else {
		message = fmt.Sprintf("An alert for `%s` has been triggered due to having failed %d time(s) in a row", endpoint.DisplayName(), alert.FailureThreshold)
	}
	for _, conditionResult := range result.ConditionResults {
		var prefix string
		if conditionResult.Success {
			prefix = "✓"
		} else {
			prefix = "✕"
		}
		results += fmt.Sprintf("\\n%s - %s", prefix, conditionResult.Condition)
	}
	var description string
	if alertDescription := alert.GetDescription(); len(alertDescription) > 0 {
		description = "\\n" + alertDescription
	}
	return fmt.Sprintf("%s%s\\n%s", message, description, results)
}

// buildHTMLMessageBody builds the message body in HTML to include in request
func buildHTMLMessageBody(endpoint *core.Endpoint, alert *alert.Alert, result *core.Result, resolved bool) string {
	var message, results string
	if resolved {
		message = fmt.Sprintf("An alert for <code>%s</code> has been resolved after passing successfully %d time(s) in a row", endpoint.DisplayName(), alert.SuccessThreshold)
	} else {
		message = fmt.Sprintf("An alert for <code>%s</code> has been triggered due to having failed %d time(s) in a row", endpoint.DisplayName(), alert.FailureThreshold)
	}
	for _, conditionResult := range result.ConditionResults {
		var prefix string
		if conditionResult.Success {
			prefix = "✅"
		} else {
			prefix = "❌"
		}
		results += fmt.Sprintf("<li>%s - <code>%s</code></li>", prefix, conditionResult.Condition)
	}
	var description string
	if alertDescription := alert.GetDescription(); len(alertDescription) > 0 {
		description = fmt.Sprintf("\\n<blockquote>%s</blockquote>", alertDescription)
	}
	return fmt.Sprintf("<h3>%s</h3>%s\\n<h5>Condition results</h5><ul>%s</ul>", message, description, results)
}

// getConfigForGroup returns the appropriate configuration for a given group
func (provider *AlertProvider) getConfigForGroup(group string) matrixProviderConfig {
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if group == override.Group {
				return matrixProviderConfig{
					HomeserverURL:  override.HomeserverURL,
					AccessToken:    override.AccessToken,
					InternalRoomID: override.InternalRoomID,
				}
			}
		}
	}
	return matrixProviderConfig{
		HomeserverURL:  provider.HomeserverURL,
		AccessToken:    provider.AccessToken,
		InternalRoomID: provider.InternalRoomID,
	}
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randStringBytes(n int) string {
	b := make([]byte, n)
	rand.Seed(time.Now().UnixNano())
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// GetDefaultAlert returns the provider's default alert configuration
func (provider AlertProvider) GetDefaultAlert() *alert.Alert {
	return provider.DefaultAlert
}
