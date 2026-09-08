package receipt

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// newTestReceipt is a test helper that creates a minimal receipt.
func newTestReceipt(id, apiKey, model string) *Receipt {
	return NewReceipt(id, apiKey, model, "test-provider", 1, 2, 3, 0)
}

func TestSigner_NewSigner(t *testing.T) {
	s1 := NewSigner(nil)
	if s1.publicKey == nil || len(s1.publicKey) != 32 {
		t.Errorf("NewSigner(nil): expected 32-byte public key, got %d bytes", len(s1.publicKey))
	}

	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	s2 := NewSigner(seed)
	s3 := NewSigner(seed)
	if s2.PublicKeyB64() != s3.PublicKeyB64() {
		t.Errorf("NewSigner with same seed: keys should be identical")
	}

	differentSeed := make([]byte, 32)
	for i := range differentSeed {
		differentSeed[i] = byte(i + 1)
	}
	s4 := NewSigner(differentSeed)
	if s2.PublicKeyB64() == s4.PublicKeyB64() {
		t.Errorf("NewSigner with different seed: keys should differ")
	}
}

func TestSigner_SignAndVerify(t *testing.T) {
	signer := NewSigner(nil)

	r := NewReceipt("receipt-1", "apikey-test", "gpt-4o", "provider-1", 10, 20, 30, 50)
	if err := signer.Sign(r); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if len(r.Signature) != ed25519.SignatureSize {
		t.Errorf("Signature length: got %d, want %d", len(r.Signature), ed25519.SignatureSize)
	}

	if !signer.Verify(r) {
		t.Errorf("Verify(signed receipt): expected true")
	}

	r.Model = "llama-3-tampered"
	if signer.Verify(r) {
		t.Errorf("Verify(tampered receipt): expected false")
	}
}

func TestSigner_Sign_AlreadySigned(t *testing.T) {
	signer := NewSigner(nil)
	r := newTestReceipt("r-id", "key", "model")
	r.Signature = []byte("already-set")

	err := signer.Sign(r)
	if err == nil {
		t.Errorf("Sign on already-signed receipt: expected error, got nil")
	}
}

func TestSigner_Verify_NilSignature(t *testing.T) {
	signer := NewSigner(nil)
	r := newTestReceipt("r-id", "key", "model")
	if signer.Verify(r) {
		t.Errorf("Verify(nil signature): expected false")
	}
}

func TestSigner_NewSignerFromFile_NotExists(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nonexistent.key")

	s1, err := NewSignerFromFile(path)
	if err != nil {
		t.Fatalf("NewSignerFromFile (new file): %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("key file not created at %s", path)
	}

	s2, err := NewSignerFromFile(path)
	if err != nil {
		t.Fatalf("NewSignerFromFile (existing file): %v", err)
	}
	if s1.PublicKeyB64() != s2.PublicKeyB64() {
		t.Errorf("NewSignerFromFile: same file should produce same key pair")
	}
}

func TestSigner_NewSignerFromFile_CorruptFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corrupt.key")

	if err := os.WriteFile(path, []byte("not-valid-base64!!!"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := NewSignerFromFile(path)
	if err == nil {
		t.Errorf("NewSignerFromFile with corrupt file: expected error, got nil")
	}
}

func TestSigner_NewSignerFromFile_InvalidSeedSize(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "badsize.key")

	shortSeed := []byte("shortseed")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(shortSeed)), 0600); err != nil {
		t.Fatalf("write badsize file: %v", err)
	}

	_, err := NewSignerFromFile(path)
	if err == nil {
		t.Errorf("NewSignerFromFile with bad seed size: expected error, got nil")
	}
}

func TestSigner_PublicKeyB64(t *testing.T) {
	signer := NewSigner(nil)
	pk := signer.PublicKeyB64()

	if len(pk) < 40 {
		t.Errorf("PublicKeyB64 length: got %d, want >= 40", len(pk))
	}

	_, err := base64.StdEncoding.DecodeString(pk)
	if err != nil {
		t.Errorf("PublicKeyB64 is not valid base64: %v", err)
	}
}

func TestSigner_PublicKey(t *testing.T) {
	signer := NewSigner(nil)
	pk := signer.PublicKey()

	if pk == nil {
		t.Errorf("PublicKey(): got nil")
	}
	if len(pk) != 32 {
		t.Errorf("PublicKey() length: got %d, want 32", len(pk))
	}

	// Should be the same key used for verification.
	if !ed25519.Verify(pk, []byte("test"), signer.SignCanonical("test")) {
		t.Errorf("PublicKey: key from PublicKey() should verify signatures")
	}
}

func TestSigner_PublicKeyMatchesVerify(t *testing.T) {
	signer := NewSigner(nil)
	r := newTestReceipt("r-id", "key", "model")
	_ = signer.Sign(r)

	seed := signer.privateKey.Seed()
	signer2 := NewSigner(seed)
	if !signer2.Verify(r) {
		t.Errorf("same-seed signer should verify receipt signed by original")
	}

	signer3 := NewSigner(nil)
	if signer3.Verify(r) {
		t.Errorf("different signer should not verify receipt")
	}
}

func TestReceipt_Canonical_Deterministic(t *testing.T) {
	r := NewReceipt("receipt-id", "apikey", "gpt-4o", "provider-x", 10, 20, 30, 50)

	c1 := r.Canonical()
	c2 := r.Canonical()
	if c1 != c2 {
		t.Errorf("Canonical() not deterministic: got %q and %q", c1, c2)
	}
}

func TestReceipt_Canonical_AllFields(t *testing.T) {
	r := NewReceipt("receipt-id", "apikey", "gpt-4o", "provider-x", 10, 20, 30, 50)
	c := r.Canonical()

	for _, want := range []string{"receipt-id", "apikey", "gpt-4o", "provider-x", "10", "20", "30", "50"} {
		if !strings.Contains(c, want) {
			t.Errorf("Canonical(): missing %q in %q", want, c)
		}
	}
}

func TestReceipt_Canonical_ChangedField(t *testing.T) {
	r := NewReceipt("r-id", "key", "model", "prov", 1, 2, 3, 4)
	c1 := r.Canonical()

	r.Model = "llama-3"
	c2 := r.Canonical()
	if c1 == c2 {
		t.Errorf("Canonical() changed field should produce different string")
	}
}

func TestHashCanonical_Deterministic(t *testing.T) {
	h1 := HashCanonical("test message")
	h2 := HashCanonical("test message")
	if h1 != h2 {
		t.Errorf("HashCanonical not deterministic: %q vs %q", h1, h2)
	}

	h3 := HashCanonical("different")
	if h1 == h3 {
		t.Errorf("HashCanonical different input: should differ")
	}
}

func TestHashCanonical_Format(t *testing.T) {
	h := HashCanonical("any content")
	if len(h) != 16 {
		t.Errorf("HashCanonical length: got %d, want 16", len(h))
	}
}

func TestInMemoryStore_SaveAndByID(t *testing.T) {
	store := NewInMemoryStore()
	signer := NewSigner(nil)

	r := newTestReceipt("receipt-1", "key", "model")
	_ = signer.Sign(r)

	if err := store.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fetched, err := store.ByID("receipt-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if fetched == nil {
		t.Fatal("ByID returned nil")
	}
	if fetched.ID != r.ID {
		t.Errorf("ByID: got id=%q, want %q", fetched.ID, r.ID)
	}
}

func TestInMemoryStore_ByID_NotFound(t *testing.T) {
	store := NewInMemoryStore()
	r, err := store.ByID("nonexistent")
	if err != nil {
		t.Fatalf("ByID(nonexistent): unexpected error: %v", err)
	}
	if r != nil {
		t.Errorf("ByID(nonexistent): expected nil receipt, got %v", r)
	}
}

func TestInMemoryStore_ByAPIKey(t *testing.T) {
	store := NewInMemoryStore()
	signer := NewSigner(nil)

	for _, r := range []*Receipt{
		newTestReceipt("r1", "key-a", "gpt-4o"),
		newTestReceipt("r2", "key-a", "llama-3"),
		newTestReceipt("r3", "key-b", "gpt-4o"),
	} {
		_ = signer.Sign(r)
		_ = store.Save(r)
	}

	byA, err := store.ByAPIKey("key-a")
	if err != nil {
		t.Fatalf("ByAPIKey: %v", err)
	}
	if len(byA) != 2 {
		t.Errorf("ByAPIKey(key-a): got %d, want 2", len(byA))
	}

	byB, err := store.ByAPIKey("key-b")
	if err != nil {
		t.Fatalf("ByAPIKey: %v", err)
	}
	if len(byB) != 1 {
		t.Errorf("ByAPIKey(key-b): got %d, want 1", len(byB))
	}

	none, err := store.ByAPIKey("key-c")
	if err != nil {
		t.Fatalf("ByAPIKey: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("ByAPIKey(unknown): got %d, want 0", len(none))
	}
}

func TestInMemoryStore_Recent(t *testing.T) {
	store := NewInMemoryStore()
	signer := NewSigner(nil)

	for i := 1; i <= 5; i++ {
		r := newTestReceipt("r"+string(rune('0'+i)), "key", "model")
		_ = signer.Sign(r)
		_ = store.Save(r)
	}

	recent3, err := store.Recent(3)
	if err != nil {
		t.Fatalf("Recent(3): %v", err)
	}
	if len(recent3) != 3 {
		t.Errorf("Recent(3): got %d, want 3", len(recent3))
	}

	recent10, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent(10): %v", err)
	}
	if len(recent10) != 5 {
		t.Errorf("Recent(10): got %d, want 5", len(recent10))
	}
}

func TestInMemoryStore_Recent_Order(t *testing.T) {
	store := NewInMemoryStore()
	signer := NewSigner(nil)

	for _, id := range []string{"r3", "r1", "r4", "r2", "r5"} {
		r := newTestReceipt(id, "key", "model")
		_ = signer.Sign(r)
		_ = store.Save(r)
	}

	recent, err := store.Recent(3)
	if err != nil {
		t.Fatalf("Recent(3): %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("Recent(3): got %d, want 3", len(recent))
	}

	// r5 was inserted last, so must be first (most recent).
	if recent[0].ID != "r5" {
		t.Errorf("Recent(3)[0]: got %q, want r5 (most recent insert)", recent[0].ID)
	}
}

func TestInMemoryStore_Concurrent(t *testing.T) {
	store := NewInMemoryStore()
	signer := NewSigner(nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := strings.Repeat(string(rune('a'+idx)), 4)
			r := newTestReceipt(id, "key", "model")
			_ = signer.Sign(r)
			_ = store.Save(r)
			_, _ = store.ByID(id)
			_, _ = store.Recent(5)
			_, _ = store.ByAPIKey("key")
		}(i)
	}
	wg.Wait()
}

func TestReceipt_MarshalJSON(t *testing.T) {
	signer := NewSigner(nil)
	r := NewReceipt("r-id", "key", "model", "prov", 5, 10, 15, 50)
	_ = signer.Sign(r)

	data, err := r.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("MarshalJSON: returned empty bytes")
	}

	s := string(data)
	if !strings.Contains(s, "signature") {
		t.Errorf("MarshalJSON: should contain 'signature' key")
	}
	if !strings.Contains(s, "r-id") {
		t.Errorf("MarshalJSON: should contain receipt ID")
	}
}

// ─── Handler tests (HTTP endpoints) ───────────────────────────────────────────

func setupTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func TestHandler_VerifyReceipt_Valid(t *testing.T) {
	signer := NewSigner(nil)
	store := NewInMemoryStore()
	handler := NewHandler(store, signer)

	r := newTestReceipt("valid-r", "key", "model")
	_ = signer.Sign(r)
	_ = store.Save(r)

	result := handler.VerifyReceipt(r)
	if !result.Valid {
		t.Errorf("VerifyReceipt(valid): expected valid=true, got false")
	}
	if result.ReceiptID != "valid-r" {
		t.Errorf("VerifyReceipt: receipt ID=%q, want valid-r", result.ReceiptID)
	}
	if result.Receipt == nil {
		t.Errorf("VerifyReceipt: Receipt should be returned on valid")
	}
}

func TestHandler_VerifyReceipt_NilSignature(t *testing.T) {
	handler := NewHandler(nil, NewSigner(nil))
	r := newTestReceipt("no-sig", "key", "model")

	result := handler.VerifyReceipt(r)
	if result.Valid {
		t.Errorf("VerifyReceipt(nil sig): expected valid=false")
	}
	if result.Message != "missing signature" {
		t.Errorf("VerifyReceipt(nil sig): message=%q, want 'missing signature'", result.Message)
	}
}

func TestHandler_VerifyReceipt_InvalidSignature(t *testing.T) {
	signer := NewSigner(nil)
	r := newTestReceipt("tampered", "key", "model")
	_ = signer.Sign(r)
	r.PromptTokens = 9999 // tamper

	handler := NewHandler(nil, signer)
	result := handler.VerifyReceipt(r)
	if result.Valid {
		t.Errorf("VerifyReceipt(tampered): expected valid=false")
	}
	if result.Message != "invalid signature" {
		t.Errorf("VerifyReceipt(tampered): message=%q", result.Message)
	}
}

func TestHandler_VerifyReceipt_NotInStore(t *testing.T) {
	signer := NewSigner(nil)
	store := NewInMemoryStore()
	handler := NewHandler(store, signer)

	r := newTestReceipt("not-stored", "key", "model")
	_ = signer.Sign(r)
	// Don't save to store.

	result := handler.VerifyReceipt(r)
	if result.Valid {
		t.Errorf("VerifyReceipt(not in store): expected valid=false")
	}
	if result.Message != "receipt not found in store" {
		t.Errorf("VerifyReceipt(not in store): message=%q", result.Message)
	}
}

func TestHandler_VerifyReceipt_NoStore(t *testing.T) {
	signer := NewSigner(nil)
	handler := NewHandler(nil, signer)

	r := newTestReceipt("no-store-r", "key", "model")
	_ = signer.Sign(r)

	// No store — should still verify valid if signature is good.
	result := handler.VerifyReceipt(r)
	if !result.Valid {
		t.Errorf("VerifyReceipt(no store, valid sig): expected valid=true")
	}
}

func TestHandler_Verify_JSON(t *testing.T) {
	signer := NewSigner(nil)
	store := NewInMemoryStore()
	handler := NewHandler(store, signer)

	r := newTestReceipt("http-verify-r", "key", "model")
	_ = signer.Sign(r)
	_ = store.Save(r)

	body, _ := json.Marshal(map[string]interface{}{"receipt": r})

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("POST", "/v1/receipts/verify", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Verify(c)

	if w.Code != http.StatusOK {
		t.Errorf("Verify: status=%d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["valid"] != true {
		t.Errorf("Verify JSON: valid=%v, want true", resp["valid"])
	}
}

func TestHandler_Verify_InvalidJSON(t *testing.T) {
	handler := NewHandler(nil, NewSigner(nil))

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("POST", "/v1/receipts/verify", strings.NewReader("{invalid}"))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Verify(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Verify(invalid JSON): status=%d, want 400", w.Code)
	}
}

func TestHandler_Verify_MissingReceipt(t *testing.T) {
	handler := NewHandler(nil, NewSigner(nil))

	body, _ := json.Marshal(map[string]interface{}{})

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("POST", "/v1/receipts/verify", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Verify(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Verify(missing receipt): status=%d, want 400", w.Code)
	}
}

func TestHandler_List_WithAPIKey(t *testing.T) {
	signer := NewSigner(nil)
	store := NewInMemoryStore()
	handler := NewHandler(store, signer)

	for _, r := range []*Receipt{
		newTestReceipt("r1", "key-a", "model"),
		newTestReceipt("r2", "key-a", "model"),
		newTestReceipt("r3", "key-b", "model"),
	} {
		_ = signer.Sign(r)
		_ = store.Save(r)
	}

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("GET", "/v1/receipts?api_key=key-a&limit=10", nil)

	handler.List(c)

	if w.Code != http.StatusOK {
		t.Errorf("List: status=%d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	receipts, ok := resp["receipts"].([]interface{})
	if !ok {
		t.Fatalf("List: receipts is not a list")
	}
	if len(receipts) != 2 {
		t.Errorf("List(api_key=key-a): got %d, want 2", len(receipts))
	}
}

func TestHandler_List_NoAPIKey(t *testing.T) {
	signer := NewSigner(nil)
	store := NewInMemoryStore()
	handler := NewHandler(store, signer)

	for i := 0; i < 3; i++ {
		r := newTestReceipt("recent-"+string(rune('0'+i)), "key", "model")
		_ = signer.Sign(r)
		_ = store.Save(r)
	}

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("GET", "/v1/receipts?limit=2", nil)

	handler.List(c)

	if w.Code != http.StatusOK {
		t.Errorf("List: status=%d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	receipts := resp["receipts"].([]interface{})
	if len(receipts) != 2 {
		t.Errorf("List(limit=2): got %d, want 2", len(receipts))
	}
}

func TestHandler_List_Empty(t *testing.T) {
	store := NewInMemoryStore()
	handler := NewHandler(store, nil)

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("GET", "/v1/receipts", nil)

	handler.List(c)

	if w.Code != http.StatusOK {
		t.Errorf("List(empty): status=%d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	receipts := resp["receipts"].([]interface{})
	if len(receipts) != 0 {
		t.Errorf("List(empty): got %d, want 0", len(receipts))
	}
}

func TestHandler_PublicKey(t *testing.T) {
	signer := NewSigner(nil)
	handler := NewHandler(nil, signer)

	c, w := setupTestContext()
	c.Request, _ = http.NewRequest("GET", "/v1/receipts/public_key", nil)

	handler.PublicKey(c)

	if w.Code != http.StatusOK {
		t.Errorf("PublicKey: status=%d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	pk, ok := resp["public_key"].(string)
	if !ok || len(pk) < 40 {
		t.Errorf("PublicKey: got %q, want non-empty base64 key", pk)
	}
}
