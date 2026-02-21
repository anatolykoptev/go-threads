package threads

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
)

func TestEncryptPassword(t *testing.T) {
	// Generate a test RSA key pair
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	keys := &encryptionKeys{
		KeyID:     42,
		PublicKey: &privKey.PublicKey,
	}

	encrypted, err := encryptPassword("test_password_123", keys)
	if err != nil {
		t.Fatalf("encryptPassword: %v", err)
	}

	// Verify format: #PWD_INSTAGRAM:4:<timestamp>:<base64>
	if !strings.HasPrefix(encrypted, "#PWD_INSTAGRAM:4:") {
		t.Errorf("encrypted = %q, want prefix #PWD_INSTAGRAM:4:", encrypted)
	}

	parts := strings.SplitN(encrypted, ":", 4)
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	if parts[0] != "#PWD_INSTAGRAM" {
		t.Errorf("parts[0] = %q", parts[0])
	}
	if parts[1] != "4" {
		t.Errorf("parts[1] = %q, want 4", parts[1])
	}
	// parts[2] = timestamp (numeric)
	if len(parts[2]) < 10 {
		t.Errorf("timestamp too short: %q", parts[2])
	}
	// parts[3] = base64 encoded payload
	if len(parts[3]) < 100 {
		t.Errorf("base64 payload too short: %q", parts[3])
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "valid token",
			body: `some bloks response text Bearer IGT:2:eyJkc191c2VyX2lkIjoiMTIzNDU2Nzg5MCJ9 more text`,
			want: "IGT:2:eyJkc191c2VyX2lkIjoiMTIzNDU2Nzg5MCJ9",
		},
		{
			name: "token with equals",
			body: `Bearer IGT:2:abc123def456ghi789+/= rest`,
			want: "IGT:2:abc123def456ghi789+/=",
		},
		{
			name:    "no token",
			body:    `{"status":"fail","message":"checkpoint_required"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := extractBearerToken([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractBearerToken: %v", err)
			}
			if token != tt.want {
				t.Errorf("token = %q, want %q", token, tt.want)
			}
		})
	}
}

func TestExtractUserIDFromLogin(t *testing.T) {
	body := []byte(`some response "pk_id":"1234567890" more data`)
	id := extractUserIDFromLogin(body)
	if id != "1234567890" {
		t.Errorf("userID = %q, want %q", id, "1234567890")
	}

	// No match
	id2 := extractUserIDFromLogin([]byte(`no pk here`))
	if id2 != "" {
		t.Errorf("userID = %q, want empty", id2)
	}
}

func TestGenerateDeviceID(t *testing.T) {
	id := generateDeviceID()
	if !strings.HasPrefix(id, "android-") {
		t.Errorf("deviceID = %q, want android- prefix", id)
	}
	if len(id) != 24 { // "android-" (8) + 16 hex chars
		t.Errorf("len(deviceID) = %d, want 24", len(id))
	}

	// Should be unique
	id2 := generateDeviceID()
	if id == id2 {
		t.Error("two device IDs should be different")
	}
}
