package availability_intake

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormaliseTwilioAddress(t *testing.T) {
	t.Run("whatsapp sender strips channel prefix", func(t *testing.T) {
		phone, channel := normaliseTwilioAddress("whatsapp:+447700900123")

		require.Equal(t, "+447700900123", phone)
		require.Equal(t, ChannelWhatsApp, channel)
	})

	t.Run("sms sender is already e164", func(t *testing.T) {
		phone, channel := normaliseTwilioAddress("+447700900123")

		require.Equal(t, "+447700900123", phone)
		require.Equal(t, ChannelSMS, channel)
	})
}

func TestTwilioSignatureMatchesKnownPayload(t *testing.T) {
	params := url.Values{
		"Body":       {"Monday and Wednesday"},
		"From":       {"whatsapp:+447700900123"},
		"MessageSid": {"SM123"},
		"To":         {"whatsapp:+15559446161"},
	}

	signature := twilioSignature(
		"https://example.ngrok-free.dev/api/v1/webhooks/twilio",
		params,
		"test-auth-token",
	)

	require.Equal(t, "YN3LKSA228YX9NfRILe/7CXHjCI=", signature)
}
