package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	testData := []byte("test data for signing")
	hash := sha256.Sum256(testData)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	if !verifySignature(&privateKey.PublicKey, testData, sig) {
		t.Error("Signature verification failed unexpectedly")
	}

	if verifySignature(&privateKey.PublicKey, []byte("wrong data"), sig) {
		t.Error("Signature verification should have failed with wrong data")
	}

	if verifySignature(&privateKey.PublicKey, testData, nil) {
		t.Error("Signature verification should have failed with nil signature")
	}

	if verifySignature(nil, testData, sig) {
		t.Error("Signature verification should have failed with nil public key")
	}
}

func TestParseAIKPublicKey(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	derBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to marshal public key: %v", err)
	}

	parsedKey, err := ParseAIKPublicKey(derBytes)
	if err != nil {
		t.Errorf("Failed to parse AIK public key: %v", err)
	}
	if parsedKey == nil {
		t.Error("Parsed key is nil")
	}

	_, err = ParseAIKPublicKey(nil)
	if err == nil {
		t.Error("Expected error for nil bytes")
	}

	_, err = ParseAIKPublicKey([]byte("invalid"))
	if err == nil {
		t.Error("Expected error for invalid bytes")
	}
}

func TestCreateNonce(t *testing.T) {
	nonce1, err := CreateNonce()
	if err != nil {
		t.Fatalf("Failed to create nonce: %v", err)
	}

	nonce2, err := CreateNonce()
	if err != nil {
		t.Fatalf("Failed to create second nonce: %v", err)
	}

	if nonce1 == nonce2 {
		t.Error("Created nonces should be different")
	}
}

func TestValidateNonce(t *testing.T) {
	var nonce1, nonce2, expected [16]byte
	for i := range nonce1 {
		nonce1[i] = byte(i)
		expected[i] = byte(i)
		nonce2[i] = byte(i + 1)
	}

	err := ValidateNonce(nonce1, expected)
	if err != nil {
		t.Errorf("Nonce validation failed for matching nonces: %v", err)
	}

	err = ValidateNonce(nonce2, expected)
	if err == nil {
		t.Error("Expected error for mismatching nonces")
	}
}

func TestMockTPMDevice(t *testing.T) {
	mock := NewMockTPMDevice()

	aiKPub, privateKey, err := GenerateMockAIK()
	if err != nil {
		t.Fatalf("Failed to generate mock AIK: %v", err)
	}

	handle := uint32(0x81000001)
	mock.SetMockAIK(handle, aiKPub)

	readAIK, err := mock.ReadAIK(handle)
	if err != nil {
		t.Errorf("Failed to read AIK: %v", err)
	}
	if string(readAIK) != string(aiKPub) {
		t.Error("AIK mismatch")
	}

	var nonce [16]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	pcrValues := make([]byte, 32)
	for i := range pcrValues {
		pcrValues[i] = byte(i)
	}

	quoteBytes, sig, err := GenerateMockQuote(privateKey, nonce, pcrValues, []int{0, 1, 2})
	if err != nil {
		t.Fatalf("Failed to generate mock quote: %v", err)
	}

	mockQuote := &MockQuote{
		QuoteBytes: quoteBytes,
		Signature:  sig,
		AIKPub:     aiKPub,
	}
	mock.SetMockQuote(handle, mockQuote)

	quoteResult, err := mock.Quote(handle, nonce, []int{0, 1, 2})
	if err != nil {
		t.Errorf("Failed to get quote: %v", err)
	}
	if string(quoteResult.QuoteBytes) != string(quoteBytes) {
		t.Error("Quote bytes mismatch")
	}

	err = mock.Close()
	if err != nil {
		t.Errorf("Failed to close mock TPM: %v", err)
	}

	_, err = mock.Quote(handle, nonce, []int{0, 1, 2})
	if err == nil {
		t.Error("Expected error for operation after close")
	}
}

func TestGenerateMockAIK(t *testing.T) {
	aiKPub, privateKey, err := GenerateMockAIK()
	if err != nil {
		t.Fatalf("Failed to generate mock AIK: %v", err)
	}

	if len(aiKPub) == 0 {
		t.Error("AIK public key is empty")
	}

	if privateKey == nil {
		t.Error("Private key is nil")
	}

	testData := []byte("test data")
	hash := sha256.Sum256(testData)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	if !verifySignature(&privateKey.PublicKey, testData, sig) {
		t.Error("Signature verification failed")
	}
}

func TestGenerateMockQuote(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	var nonce [16]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}

	pcrValues := make([]byte, 32)
	for i := range pcrValues {
		pcrValues[i] = byte(i)
	}

	quoteBytes, sig, err := GenerateMockQuote(privateKey, nonce, pcrValues, []int{0, 1, 2})
	if err != nil {
		t.Fatalf("Failed to generate mock quote: %v", err)
	}

	if len(quoteBytes) == 0 {
		t.Error("Quote bytes are empty")
	}

	if len(sig) == 0 {
		t.Error("Signature is empty")
	}

	if !verifySignature(&privateKey.PublicKey, quoteBytes, sig) {
		t.Error("Quote signature verification failed")
	}
}

func TestVerifyQuoteWithSignature(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	var nonce [16]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	var pcrHash [32]byte
	for i := range pcrHash {
		pcrHash[i] = byte(i)
	}

	q := QuoteData{
		Nonce:     nonce,
		SessionID: "test-session",
		Timestamp: time.Now().Unix(),
		PCRHash:   pcrHash,
	}

	dataBytes, err := encodeQuoteData(q)
	if err != nil {
		t.Fatalf("Failed to encode quote data: %v", err)
	}

	hash := sha256.Sum256(dataBytes)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	quote := Quote{
		QuoteData: q,
		Signature: sig,
	}

	err = VerifyQuote(&privateKey.PublicKey, quote, nonce, "test-session", "", time.Now())
	if err != nil {
		t.Errorf("Quote verification failed unexpectedly: %v", err)
	}

	wrongNonce := nonce
	wrongNonce[0] ^= 0xFF
	err = VerifyQuote(&privateKey.PublicKey, quote, wrongNonce, "test-session", "", time.Now())
	if err == nil {
		t.Error("Expected error for nonce mismatch")
	}

	staleQ := QuoteData{
		Nonce:     nonce,
		SessionID: "test-session",
		Timestamp: time.Now().Add(-10 * time.Minute).Unix(),
		PCRHash:   pcrHash,
	}
	staleQuote := Quote{
		QuoteData: staleQ,
		Signature: sig,
	}
	err = VerifyQuote(&privateKey.PublicKey, staleQuote, nonce, "test-session", "", time.Now())
	if err == nil {
		t.Error("Expected error for stale timestamp")
	}
}

func TestNewTPMDevice(t *testing.T) {
	device, err := NewTPMDevice()
	if err != nil {
		t.Logf("NewTPMDevice failed (expected on some platforms): %v", err)
		return
	}
	if device != nil {
		device.Close()
	}
}

func TestPCRValues(t *testing.T) {
	pcrValues := NewPCRValues()

	val1 := make([]byte, 32)
	val2 := make([]byte, 32)
	for i := range val1 {
		val1[i] = byte(i)
		val2[i] = byte(i + 1)
	}
	pcrValues.Set(0, val1)
	pcrValues.Set(1, val2)

	got1, ok := pcrValues.Get(0)
	if !ok {
		t.Error("Failed to get PCR 0")
	}
	if string(got1) != string(val1) {
		t.Error("PCR 0 value mismatch")
	}

	_, ok = pcrValues.Get(2)
	if ok {
		t.Error("Should not find PCR 2")
	}
}

func TestPCRSelection(t *testing.T) {
	sel := NewPCRSelection(PCRBankSHA256, []int{0, 1, 2})

	if len(sel.PCRs) != 3 {
		t.Errorf("Expected 3 PCRs, got %d", len(sel.PCRs))
	}

	expectedBitmap := [4]byte{0x07, 0, 0, 0}
	if sel.Bitmap != expectedBitmap {
		t.Errorf("Bitmap mismatch: got %x, expected %x", sel.Bitmap, expectedBitmap)
	}

	info, err := GetPCRBankInfo(PCRBankSHA256)
	if err != nil {
		t.Errorf("Failed to get PCR bank info: %v", err)
	}
	if info.HashAlgName != "SHA256" {
		t.Errorf("Expected SHA256, got %s", info.HashAlgName)
	}

	_, err = GetPCRBankInfo(999)
	if err == nil {
		t.Error("Expected error for unknown bank")
	}
}

func TestPCRFingerprint(t *testing.T) {
	pcrValues := NewPCRValues()
	val := make([]byte, 32)
	for i := range val {
		val[i] = byte(i)
	}
	pcrValues.Set(0, val)
	pcrValues.Set(1, val)

	sel := NewPCRSelection(PCRBankSHA256, []int{0, 1})

	fp1 := PCRFingerprint(pcrValues, sel)
	fp2 := PCRFingerprint(pcrValues, sel)

	if fp1 != fp2 {
		t.Error("Same values should produce same fingerprint")
	}

	sel2 := NewPCRSelection(PCRBankSHA256, []int{0})
	fp3 := PCRFingerprint(pcrValues, sel2)
	if fp1 == fp3 {
		t.Error("Different selections should produce different fingerprint")
	}
}

func TestVerifyPCRValues(t *testing.T) {
	pcrValues := NewPCRValues()
	val := make([]byte, 32)
	for i := range val {
		val[i] = byte(i)
	}
	pcrValues.Set(0, val)
	pcrValues.Set(1, val)
	pcrValues.Set(2, val)

	sel := NewPCRSelection(PCRBankSHA256, []int{0, 1, 2})
	digest := pcrValues.PCRDigest(sel)
	var digestArray [32]byte
	copy(digestArray[:], digest)

	err := VerifyPCRValues(pcrValues, sel, digestArray)
	if err != nil {
		t.Errorf("PCR verification failed unexpectedly: %v", err)
	}

	wrongDigest := digestArray
	wrongDigest[0] ^= 0xFF
	err = VerifyPCRValues(pcrValues, sel, wrongDigest)
	if err == nil {
		t.Error("Expected error for wrong digest")
	}
}

func TestCommonPCRSelection(t *testing.T) {
	sel := CommonPCRSelection()
	if len(sel.PCRs) != 8 {
		t.Errorf("Expected 8 PCRs, got %d", len(sel.PCRs))
	}
}

func generateTestCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
			CommonName:   "Test Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, err
	}

	return cert, privateKey, nil
}

func generateAIKCertificate(ca *x509.Certificate, caKey *ecdsa.PrivateKey, aikPublicKey *ecdsa.PublicKey) (*x509.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Organization"},
			CommonName:   "AIK Certificate",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, ca, aikPublicKey, caKey)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(certBytes)
}

func TestVerifyAIKCertificateChain(t *testing.T) {
	rootCA, rootKey, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	aikKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate AIK key: %v", err)
	}

	aikCert, err := generateAIKCertificate(rootCA, rootKey, &aikKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to generate AIK certificate: %v", err)
	}

	err = VerifyAIKCertificateChain(aikCert, []*x509.Certificate{rootCA}, nil)
	if err != nil {
		t.Errorf("Certificate chain verification failed: %v", err)
	}

	err = VerifyAIKCertificateChain(nil, []*x509.Certificate{rootCA}, nil)
	if err == nil {
		t.Error("Expected error for nil certificate")
	}

	err = VerifyAIKCertificateChain(aikCert, nil, nil)
	if err == nil {
		t.Error("Expected error for no trusted roots")
	}
}

func TestParseAIKCertificate(t *testing.T) {
	rootCA, rootKey, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	aikKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate AIK key: %v", err)
	}

	aikCert, err := generateAIKCertificate(rootCA, rootKey, &aikKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to generate AIK certificate: %v", err)
	}

	cert, err := ParseAIKCertificate(aikCert.Raw)
	if err != nil {
		t.Errorf("Failed to parse AIK certificate: %v", err)
	}
	if cert == nil {
		t.Error("Parsed certificate is nil")
	}

	_, err = ParseAIKCertificate(nil)
	if err == nil {
		t.Error("Expected error for empty bytes")
	}

	_, err = ParseAIKCertificate([]byte("invalid"))
	if err == nil {
		t.Error("Expected error for invalid bytes")
	}
}

func TestVerifyQuoteError(t *testing.T) {
	err := &VerifyQuoteError{
		Code:    ErrQuoteSignatureInvalid,
		Message: "test error",
	}

	expected := "tpm quote verification failed (1): test error"
	if err.Error() != expected {
		t.Errorf("Error message mismatch: got %s, expected %s", err.Error(), expected)
	}
}

func TestParsePCREvent(t *testing.T) {
	eventBytes := make([]byte, 21)
	binary.BigEndian.PutUint32(eventBytes[0:4], 0x00000003)
	binary.BigEndian.PutUint32(eventBytes[4:8], 7)
	binary.BigEndian.PutUint32(eventBytes[8:12], 8)
	for i := 0; i < 8; i++ {
		eventBytes[12+i] = byte(i)
	}
	eventBytes[20] = 0xAA

	pcrIndex, eventType, data, digest, err := ParsePCREvent(eventBytes)
	if err != nil {
		t.Fatalf("ParsePCREvent failed: %v", err)
	}

	if pcrIndex != 7 {
		t.Errorf("Expected PCR 7, got %d", pcrIndex)
	}
	if eventType != 0x00000003 {
		t.Errorf("Expected event type 0x00000003, got 0x%x", eventType)
	}
	if len(digest) != 8 {
		t.Errorf("Expected digest len 8, got %d", len(digest))
	}
	if data[0] != 0xAA {
		t.Error("Data mismatch")
	}

	_, _, _, _, err = ParsePCREvent([]byte{1, 2})
	if err == nil {
		t.Error("Expected error for short event")
	}
}
