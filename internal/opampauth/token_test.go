package opampauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestGenerateProducesStrictTokenFormat(t *testing.T) {
	generated, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	parts := strings.Split(generated.Value, ".")
	if len(parts) != 2 {
		t.Fatalf("token parts = %d, want 2", len(parts))
	}
	if !strings.HasPrefix(parts[0], "ompt_") {
		t.Fatalf("token prefix = %q, want ompt_", parts[0])
	}

	id := strings.TrimPrefix(parts[0], "ompt_")
	parsedID, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("UUID parse error = %v", err)
	}
	if id != parsedID.String() || generated.ID != id {
		t.Fatalf("ID = %q, token UUID = %q, want canonical lowercase UUID", generated.ID, id)
	}

	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("secret decode error = %v", err)
	}
	if len(parts[1]) != 43 || len(secret) != 32 {
		t.Fatalf("secret length = encoded %d, decoded %d; want 43 and 32", len(parts[1]), len(secret))
	}
	if strings.Contains(parts[1], "=") {
		t.Fatal("secret must not contain base64 padding")
	}
}

func TestGenerateProducesUniqueTokensAndHashOfFullValue(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if first.Value == second.Value || first.ID == second.ID || first.SecretHash == second.SecretHash {
		t.Fatal("two generated tokens must differ")
	}

	wantHash := sha256.Sum256([]byte(first.Value))
	if first.SecretHash != wantHash {
		t.Fatal("SecretHash must be SHA-256 of the complete token value")
	}
}

func TestParseAndHashRoundTripsGeneratedToken(t *testing.T) {
	generated, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	id, hash, err := ParseAndHash(generated.Value)
	if err != nil {
		t.Fatalf("ParseAndHash() error = %v", err)
	}
	if id != generated.ID {
		t.Fatalf("ID = %q, want %q", id, generated.ID)
	}
	if hash != generated.SecretHash {
		t.Fatal("hash does not match generated SecretHash")
	}
}

func TestParseAndHashRejectsMalformedTokensWithoutDisclosure(t *testing.T) {
	validID := "7b3b1234-5678-4abc-8def-1234567890ab"
	validSecret := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	valid := "ompt_" + validID + "." + validSecret

	tests := []string{
		"",
		"ompt_" + validID,
		"opmt_" + validID + "." + validSecret,
		"ompt-" + validID + "." + validSecret,
		"ompt_" + strings.ToUpper(validID) + "." + validSecret,
		"ompt_" + "7b3b123456784abc8def1234567890ab" + "." + validSecret,
		"ompt_" + validID + "." + validSecret + "=",
		"ompt_" + validID + "." + strings.Repeat("a", 42),
		"ompt_" + validID + "." + strings.Repeat("a", 44),
		"ompt_" + validID + "." + strings.Repeat("!", 43),
		valid + ".extra",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, _, err := ParseAndHash(value)
			if err == nil {
				t.Fatal("ParseAndHash() error = nil, want invalid token error")
			}
			if err.Error() != "invalid OpAMP token" {
				t.Fatalf("error = %q, want generic invalid token error", err)
			}
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatalf("error discloses supplied token: %q", err)
			}
		})
	}
}

func TestOpAMPTokenPublicModel(t *testing.T) {
	now := time.Now().UTC()
	credential := models.OpAMPTokenCredential{
		Token: models.OpAMPToken{
			ID: "token-1", Name: "production", Description: "production collector", Team: "platform",
			Environment: "production", CreatedAt: now, CreatedBy: "user-1", ExpiresAt: &now,
			LastUsedAt: &now, RevokedAt: &now, RevokedBy: "user-2", Status: models.OpAMPTokenActive,
		},
		SecretHash: sha256.Sum256([]byte("token")),
	}
	principal := models.OpAMPTokenPrincipal{ID: credential.Token.ID, ExpiresAt: credential.Token.ExpiresAt}
	if principal.ID != "token-1" || credential.Token.Status != models.OpAMPTokenActive {
		t.Fatal("public OpAMP token model does not retain its values")
	}
}
