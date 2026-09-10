package trust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"runtime"
	"sync"
	"time"
)

type TPMDevice interface {
	Quote(handle uint32, nonce [16]byte, pcrSelection []int) (*TPMQuoteResult, error)
	ReadAIK(handle uint32) ([]byte, error)
	GetRandom(len uint16) ([]byte, error)
	Close() error
}

type TPMQuoteResult struct {
	QuoteBytes []byte
	Signature  []byte
	AIKPub     []byte
	Timestamp  uint64
	ClockInfo  [17]byte
}

type LinuxTPMDevice struct {
	file   *os.File
	mu     sync.Mutex
	isOpen bool
}

func NewTPMDevice() (TPMDevice, error) {
	switch runtime.GOOS {
	case "linux":
		return NewLinuxTPMDevice("/dev/tpmrm0")
	case "windows":
		return NewWindowsTPMDevice()
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func NewLinuxTPMDevice(path string) (*LinuxTPMDevice, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open TPM device %s: %w", path, err)
	}
	return &LinuxTPMDevice{file: file, isOpen: true}, nil
}

func (d *LinuxTPMDevice) Quote(handle uint32, nonce [16]byte, pcrSelection []int) (*TPMQuoteResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isOpen {
		return nil, errors.New("TPM device is closed")
	}
	cmd := buildQuoteCommand(handle, nonce, pcrSelection)
	response, err := d.sendCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("TPM quote failed: %w", err)
	}
	return parseQuoteResponse(response)
}

func (d *LinuxTPMDevice) ReadAIK(handle uint32) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isOpen {
		return nil, errors.New("TPM device is closed")
	}
	cmd := buildReadPublicCommand(handle)
	response, err := d.sendCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("TPM ReadAIK failed: %w", err)
	}
	return parsePublicArea(response)
}

func (d *LinuxTPMDevice) GetRandom(len uint16) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.isOpen {
		return nil, errors.New("TPM device is closed")
	}
	cmd := buildGetRandomCommand(len)
	response, err := d.sendCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("TPM GetRandom failed: %w", err)
	}
	return parseGetRandomResponse(response)
}

func (d *LinuxTPMDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil {
		d.isOpen = false
		return d.file.Close()
	}
	return nil
}

func (d *LinuxTPMDevice) sendCommand(cmd []byte) ([]byte, error) {
	_, err := d.file.Write(cmd)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 10)
	_, err = io.ReadFull(d.file, header)
	if err != nil {
		return nil, err
	}
	respSize := binary.BigEndian.Uint32(header[2:6])
	if respSize > 4096 {
		return nil, errors.New("response size too large")
	}
	response := make([]byte, respSize-10)
	_, err = io.ReadFull(d.file, response)
	if err != nil {
		return nil, err
	}
	return append(header, response...), nil
}

const (
	TPM2_CC_Quote      = 0x8000007F
	TPM2_CC_ReadPublic = 0x8000001E
	TPM2_CC_GetRandom  = 0x0000017B
)

func buildQuoteCommand(handle uint32, nonce [16]byte, pcrSelection []int) []byte {
	cmd := make([]byte, 10)
	binary.BigEndian.PutUint16(cmd[0:2], 0x8002)
	binary.BigEndian.PutUint32(cmd[6:10], TPM2_CC_Quote)
	handleBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(handleBytes, handle)
	cmd = append(cmd, handleBytes...)
	nonceHash := sha256.Sum256(nonce[:])
	cmd = append(cmd, nonceHash[:]...)
	pcrSel := buildPCRSelection(pcrSelection)
	cmd = append(cmd, pcrSel...)
	return cmd
}

func buildReadPublicCommand(handle uint32) []byte {
	cmd := make([]byte, 10)
	binary.BigEndian.PutUint16(cmd[0:2], 0x8002)
	binary.BigEndian.PutUint32(cmd[6:10], TPM2_CC_ReadPublic)
	handleBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(handleBytes, handle)
	return append(cmd, handleBytes...)
}

func buildGetRandomCommand(len uint16) []byte {
	cmd := make([]byte, 10)
	binary.BigEndian.PutUint16(cmd[0:2], 0x8002)
	binary.BigEndian.PutUint32(cmd[6:10], TPM2_CC_GetRandom)
	lenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, len)
	return append(cmd, lenBytes...)
}

func buildPCRSelection(pcrs []int) []byte {
	sel := make([]byte, 9)
	binary.BigEndian.PutUint16(sel[0:2], 9)
	binary.BigEndian.PutUint16(sel[2:4], 0x000B)
	sel[4] = 24
	for _, pcr := range pcrs {
		if pcr >= 0 && pcr < 24 {
			sel[5+pcr/8] |= 1 << (pcr % 8)
		}
	}
	return sel
}

func parseQuoteResponse(resp []byte) (*TPMQuoteResult, error) {
	if len(resp) < 10 {
		return nil, errors.New("response too short")
	}
	returnCode := binary.BigEndian.Uint32(resp[6:10])
	if returnCode != 0 {
		return nil, fmt.Errorf("TPM error: 0x%x", returnCode)
	}
	offset := 10
	quoteSize := binary.BigEndian.Uint32(resp[offset : offset+4])
	offset += 4
	quoteBytes := resp[offset : offset+int(quoteSize)]
	offset += int(quoteSize)
	sigSize := binary.BigEndian.Uint32(resp[offset : offset+4])
	offset += 4
	signature := resp[offset : offset+int(sigSize)]
	return &TPMQuoteResult{QuoteBytes: quoteBytes, Signature: signature}, nil
}

func parsePublicArea(resp []byte) ([]byte, error) {
	if len(resp) < 14 {
		return nil, errors.New("response too short")
	}
	offset := 10
	pubSize := binary.BigEndian.Uint32(resp[offset : offset+4])
	offset += 4
	if offset+int(pubSize) > len(resp) {
		return nil, errors.New("invalid public area size")
	}
	return resp[offset : offset+int(pubSize)], nil
}

func parseGetRandomResponse(resp []byte) ([]byte, error) {
	if len(resp) < 14 {
		return nil, errors.New("response too short")
	}
	offset := 10
	randSize := binary.BigEndian.Uint32(resp[offset : offset+4])
	offset += 4
	if offset+int(randSize) > len(resp) {
		return nil, errors.New("invalid random size")
	}
	return resp[offset : offset+int(randSize)], nil
}

type WindowsTPMDevice struct {
	context uint32
	isOpen  bool
}

func NewWindowsTPMDevice() (*WindowsTPMDevice, error) {
	return nil, errors.New("Windows TPM not yet implemented - requires TBS API")
}

func (d *WindowsTPMDevice) Quote(handle uint32, nonce [16]byte, pcrSelection []int) (*TPMQuoteResult, error) {
	return nil, errors.New("Windows TPM not implemented")
}

func (d *WindowsTPMDevice) ReadAIK(handle uint32) ([]byte, error) {
	return nil, errors.New("Windows TPM not implemented")
}

func (d *WindowsTPMDevice) GetRandom(len uint16) ([]byte, error) {
	return nil, errors.New("Windows TPM not implemented")
}

func (d *WindowsTPMDevice) Close() error {
	return nil
}

type MockTPMDevice struct {
	mu           sync.Mutex
	quotes       map[uint32]*MockQuote
	aiks         map[uint32][]byte
	randomStream []byte
	randPos      int
	closed       bool
}

type MockQuote struct {
	QuoteBytes []byte
	Signature  []byte
	AIKPub     []byte
}

func NewMockTPMDevice() *MockTPMDevice {
	return &MockTPMDevice{
		quotes: make(map[uint32]*MockQuote),
		aiks:   make(map[uint32][]byte),
	}
}

func (m *MockTPMDevice) SetMockQuote(handle uint32, quote *MockQuote) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotes[handle] = quote
}

func (m *MockTPMDevice) SetMockAIK(handle uint32, aiKPub []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.aiks[handle] = aiKPub
}

func (m *MockTPMDevice) Quote(handle uint32, nonce [16]byte, pcrSelection []int) (*TPMQuoteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("mock TPM is closed")
	}
	quote, ok := m.quotes[handle]
	if !ok {
		return nil, errors.New("no mock quote configured for handle")
	}
	return &TPMQuoteResult{QuoteBytes: quote.QuoteBytes, Signature: quote.Signature, AIKPub: quote.AIKPub}, nil
}

func (m *MockTPMDevice) ReadAIK(handle uint32) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("mock TPM is closed")
	}
	aiKPub, ok := m.aiks[handle]
	if !ok {
		return nil, errors.New("no mock AIK configured for handle")
	}
	return aiKPub, nil
}

func (m *MockTPMDevice) GetRandom(len uint16) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("mock TPM is closed")
	}
	result := make([]byte, len)
	for i := range result {
		result[i] = byte(i)
	}
	return result, nil
}

func (m *MockTPMDevice) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func GenerateMockAIK() ([]byte, *ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return publicKeyBytes, privateKey, nil
}

func GenerateMockQuote(privateKey *ecdsa.PrivateKey, nonce [16]byte, pcrValues []byte, pcrSelection []int) ([]byte, []byte, error) {
	quoteData := buildMockQuoteData(nonce, pcrValues)
	hash := sha256.Sum256(quoteData)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		return nil, nil, err
	}
	return quoteData, sig, nil
}

func buildMockQuoteData(nonce [16]byte, pcrValues []byte) []byte {
	var data []byte
	data = append(data, 0x80, 0x16)
	data = append(data, 0x80, 0x17)
	qs := nonce[:]
	qsLen := make([]byte, 2)
	binary.BigEndian.PutUint16(qsLen, uint16(len(qs)))
	data = append(data, qsLen...)
	data = append(data, qs...)
	clockInfo := make([]byte, 17)
	binary.BigEndian.PutUint64(clockInfo[8:], uint64(time.Now().Unix()*1000))
	data = append(data, clockInfo...)
	fwVer := make([]byte, 8)
	data = append(data, fwVer...)
	pcrHash := sha256.Sum256(pcrValues)
	pcrLen := make([]byte, 2)
	binary.BigEndian.PutUint16(pcrLen, uint16(len(pcrHash)))
	data = append(data, pcrLen...)
	data = append(data, pcrHash[:]...)
	return data
}

type TPMVerifier struct {
	device       TPMDevice
	trustedRoots []*x509.Certificate
}

func NewTPMVerifier(device TPMDevice, trustedRoots []*x509.Certificate) *TPMVerifier {
	return &TPMVerifier{device: device, trustedRoots: trustedRoots}
}

type QuoteVerificationInput struct {
	AIKHandle      uint32
	AIKCert        []byte
	AIKPublicKey   *ecdsa.PublicKey
	QuoteBytes     []byte
	Signature      []byte
	ExpectedNonce  [16]byte
	SessionID      string
	PCRFingerprint string
	PCRSelect      []int
}

type QuoteVerificationResult struct {
	Verified       bool
	TrustTier      TrustTier
	PCRFingerprint string
	Timestamp      int64
	Error          error
}

func (v *TPMVerifier) VerifyQuote(input QuoteVerificationInput, now time.Time) (*QuoteVerificationResult, error) {
	result := &QuoteVerificationResult{Verified: false}
	if len(input.AIKCert) > 0 {
		cert, err := ParseAIKCertificate(input.AIKCert)
		if err != nil {
			result.Error = fmt.Errorf("AIK certificate parsing failed: %w", err)
			return result, result.Error
		}
		if err := VerifyAIKCertificateChain(cert, v.trustedRoots, nil); err != nil {
			result.Error = fmt.Errorf("AIK certificate chain verification failed: %w", err)
			return result, result.Error
		}
	}
	quoteData, err := ParseQuoteDataFromTPM(input.QuoteBytes, input.ExpectedNonce)
	if err != nil {
		result.Error = fmt.Errorf("quote data parsing failed: %w", err)
		return result, result.Error
	}
	quote := Quote{QuoteData: *quoteData, Signature: input.Signature}
	var pub crypto.PublicKey
	if input.AIKPublicKey != nil {
		pub = input.AIKPublicKey
	} else {
		result.Error = errors.New("no AIK public key available")
		return result, result.Error
	}
	if err := VerifyQuote(pub, quote, input.ExpectedNonce, input.SessionID, input.PCRFingerprint, now); err != nil {
		result.Error = err
		return result, result.Error
	}
	result.Verified = true
	result.TrustTier = TierHardware
	result.PCRFingerprint = fmt.Sprintf("%x", sha256.Sum256(quoteData.PCRHash[:]))
	result.Timestamp = quoteData.Timestamp
	return result, nil
}

func (v *TPMVerifier) AttestQuote(input QuoteVerificationInput, now time.Time) (*QuoteVerificationResult, error) {
	result := &QuoteVerificationResult{Verified: false}
	quoteResult, err := v.device.Quote(input.AIKHandle, input.ExpectedNonce, input.PCRSelect)
	if err != nil {
		result.Error = fmt.Errorf("TPM quote request failed: %w", err)
		return result, result.Error
	}
	aiKPub, err := v.device.ReadAIK(input.AIKHandle)
	if err != nil {
		result.Error = fmt.Errorf("failed to read AIK public key: %w", err)
		return result, result.Error
	}
	ecdsaPub, err := ParseAIKPublicKey(aiKPub)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse AIK public key: %w", err)
		return result, result.Error
	}
	hash := sha256.Sum256(quoteResult.QuoteBytes)
	var parsedSig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(quoteResult.Signature, &parsedSig); err != nil {
		result.Error = fmt.Errorf("failed to parse signature: %w", err)
		return result, result.Error
	}
	if !ecdsa.Verify(ecdsaPub, hash[:], parsedSig.R, parsedSig.S) {
		result.Error = errors.New("quote signature verification failed")
		return result, result.Error
	}
	quoteData, err := ParseQuoteDataFromTPM(quoteResult.QuoteBytes, input.ExpectedNonce)
	if err != nil {
		result.Error = fmt.Errorf("quote data parsing failed: %w", err)
		return result, result.Error
	}
	if quoteData.Nonce != input.ExpectedNonce {
		result.Error = errors.New("nonce mismatch in quote")
		return result, result.Error
	}
	age := now.Sub(time.Unix(quoteData.Timestamp, 0))
	if age < 0 {
		age = -age
	}
	if age > 5*time.Minute {
		result.Error = errors.New("quote timestamp too old")
		return result, result.Error
	}
	result.Verified = true
	result.TrustTier = TierHardware
	result.PCRFingerprint = fmt.Sprintf("%x", sha256.Sum256(quoteData.PCRHash[:]))
	result.Timestamp = quoteData.Timestamp
	return result, nil
}

