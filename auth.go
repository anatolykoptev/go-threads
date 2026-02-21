package threads

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// encryptionKeys holds Instagram's public key for password encryption.
type encryptionKeys struct {
	KeyID     int
	PublicKey *rsa.PublicKey
}

// fetchEncryptionKeys retrieves the RSA public key from Instagram's qe/sync endpoint.
func (c *Client) fetchEncryptionKeys() (*encryptionKeys, error) {
	headers := map[string]string{
		"User-Agent":   barcelonaUA,
		"Content-Type": "application/x-www-form-urlencoded",
		"X-IG-App-ID":  igAppID,
	}

	form := url.Values{}
	form.Set("id", generateDeviceID())
	form.Set("experiments", "ig_android_fci,ig_android_device_detection_info_upload,ig_android_device_info_foreground_reporting,ig_android_enable_automatic_session_marker_logging,ig_android_background_keepalive_idle,ig_android_sso_kototoro_app_page")

	body, respHeaders, status, err := c.do("POST", igBaseURL+pathEncryptionSync, headers, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("fetch encryption keys: %w", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("fetch encryption keys: HTTP %d: %s", status, truncateBytes(body, 200))
	}

	keyIDStr := respHeaders["ig-set-password-encryption-key-id"]
	pubKeyStr := respHeaders["ig-set-password-encryption-pub-key"]
	if keyIDStr == "" || pubKeyStr == "" {
		// Try lowercase header names (HTTP/2 lowercases)
		keyIDStr = respHeaders["Ig-Set-Password-Encryption-Key-Id"]
		pubKeyStr = respHeaders["Ig-Set-Password-Encryption-Pub-Key"]
	}
	if keyIDStr == "" || pubKeyStr == "" {
		return nil, fmt.Errorf("encryption key headers not found in response")
	}

	keyID, err := strconv.Atoi(keyIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse key ID %q: %w", keyIDStr, err)
	}

	pubKey, err := parseRSAPublicKey(pubKeyStr)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	return &encryptionKeys{KeyID: keyID, PublicKey: pubKey}, nil
}

// parseRSAPublicKey parses a base64/PEM-encoded RSA public key.
func parseRSAPublicKey(keyStr string) (*rsa.PublicKey, error) {
	// Try raw base64 first (most common from Instagram)
	keyBytes, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		// Try PEM format
		block, _ := pem.Decode([]byte(keyStr))
		if block == nil {
			return nil, fmt.Errorf("failed to decode key: neither base64 nor PEM")
		}
		keyBytes = block.Bytes
	}

	pub, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not RSA")
	}
	return rsaPub, nil
}

// encryptPassword encrypts a password using Instagram's hybrid RSA+AES-GCM scheme.
// Output format: #PWD_INSTAGRAM:4:<timestamp>:<base64>
func encryptPassword(password string, keys *encryptionKeys) (string, error) {
	ts := time.Now().Unix()
	tsStr := strconv.FormatInt(ts, 10)

	// Generate AES-256 key (32 bytes) and IV (12 bytes)
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return "", fmt.Errorf("generate AES key: %w", err)
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate IV: %w", err)
	}

	// RSA-PKCS1v15 encrypt the AES key
	encryptedAESKey, err := rsa.EncryptPKCS1v15(rand.Reader, keys.PublicKey, aesKey)
	if err != nil {
		return "", fmt.Errorf("RSA encrypt: %w", err)
	}

	// AES-GCM encrypt the password with timestamp as AAD
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM: %w", err)
	}

	// Encrypt with timestamp as additional authenticated data
	aad := []byte(tsStr)
	ciphertext := gcm.Seal(nil, iv, []byte(password), aad)

	// Split ciphertext into actual ciphertext and GCM tag (last 16 bytes)
	tagSize := gcm.Overhead()
	encryptedPassword := ciphertext[:len(ciphertext)-tagSize]
	gcmTag := ciphertext[len(ciphertext)-tagSize:]

	// Build binary envelope:
	// [1B version=1] [1B keyID] [12B IV] [2B RSA key length (LE)] [NB encrypted AES key] [16B GCM tag] [NB ciphertext]
	rsaKeyLen := len(encryptedAESKey)
	envelope := make([]byte, 0, 1+1+12+2+rsaKeyLen+tagSize+len(encryptedPassword))
	envelope = append(envelope, 1)             // version
	envelope = append(envelope, byte(keys.KeyID)) // key ID
	envelope = append(envelope, iv...)         // 12B IV
	rsaLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(rsaLenBytes, uint16(rsaKeyLen))
	envelope = append(envelope, rsaLenBytes...)     // 2B RSA key length
	envelope = append(envelope, encryptedAESKey...) // encrypted AES key
	envelope = append(envelope, gcmTag...)          // 16B GCM tag
	envelope = append(envelope, encryptedPassword...) // ciphertext

	encoded := base64.StdEncoding.EncodeToString(envelope)
	return fmt.Sprintf("#PWD_INSTAGRAM:4:%s:%s", tsStr, encoded), nil
}

// Login authenticates with Instagram using username/password.
func (c *Client) Login(ctx context.Context) error {
	if c.cfg.Username == "" || c.cfg.Password == "" {
		return fmt.Errorf("Login: username and password required")
	}

	// Step 1: Fetch encryption keys
	keys, err := c.fetchEncryptionKeys()
	if err != nil {
		return fmt.Errorf("Login: %w", err)
	}

	// Step 2: Encrypt password
	encPassword, err := encryptPassword(c.cfg.Password, keys)
	if err != nil {
		return fmt.Errorf("Login: %w", err)
	}

	// Step 3: POST login request via Bloks
	deviceID := generateDeviceID()
	form := url.Values{}
	form.Set("params", fmt.Sprintf(
		`{"client_input_params":{"password":"%s","contact_point":"%s","device_id":"%s"},"server_params":{"credential_type":"password","device_id":"%s"}}`,
		encPassword, c.cfg.Username, deviceID, deviceID,
	))

	headers := map[string]string{
		"User-Agent":   barcelonaUA,
		"Content-Type": "application/x-www-form-urlencoded",
		"X-IG-App-ID":  igAppID,
	}

	body, _, status, err := c.do("POST", igBaseURL+pathBloksLogin, headers, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("Login: request: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("Login: HTTP %d: %s", status, truncateBytes(body, 300))
	}

	// Step 4: Extract bearer token from Bloks response
	token, err := extractBearerToken(body)
	if err != nil {
		return fmt.Errorf("Login: %w", err)
	}

	userID := extractUserIDFromLogin(body)

	c.authMu.Lock()
	c.token = token
	c.userID = userID
	c.authMu.Unlock()

	return nil
}

var bearerTokenRe = regexp.MustCompile(`Bearer (IGT:2:[A-Za-z0-9+/=]+)`)

// extractBearerToken extracts the IGT:2 token from a Bloks login response.
func extractBearerToken(body []byte) (string, error) {
	matches := bearerTokenRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("bearer token not found in login response")
	}
	return string(matches[1]), nil
}

var loginUserIDRe = regexp.MustCompile(`"pk_id":"(\d+)"`)

// extractUserIDFromLogin extracts the user ID from a Bloks login response.
func extractUserIDFromLogin(body []byte) string {
	matches := loginUserIDRe.FindSubmatch(body)
	if len(matches) >= 2 {
		return string(matches[1])
	}
	return ""
}

// generateDeviceID creates a consistent device ID for API requests.
func generateDeviceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("android-%x", b)
}
