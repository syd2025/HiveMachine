package trust

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	PCRBankSHA256 = iota
	PCRBankSHA1
	PCRBankSHA384
	PCRBankSM3
)

const (
	PCR0  = 0
	PCR1  = 1
	PCR2  = 2
	PCR3  = 3
	PCR4  = 4
	PCR5  = 5
	PCR6  = 6
	PCR7  = 7
	PCR8  = 8
	PCR9  = 9
	PCR10 = 10
	PCR11 = 11
	PCR12 = 12
	PCR13 = 13
	PCR14 = 14
	PCR15 = 15
	PCR16 = 16
	PCR17 = 17
	PCR18 = 18
	PCR23 = 23
)

type PCRSelection struct {
	Bank   int
	PCRs   []int
	Bitmap [4]byte
}

func NewPCRSelection(bank int, pcrs []int) *PCRSelection {
	sel := &PCRSelection{Bank: bank, PCRs: make([]int, len(pcrs))}
	copy(sel.PCRs, pcrs)
	for _, pcr := range pcrs {
		if pcr >= 0 && pcr < 24 {
			sel.Bitmap[pcr/8] |= 1 << (pcr % 8)
		}
	}
	return sel
}

type PCRValues struct {
	Values map[int][]byte
}

func NewPCRValues() *PCRValues {
	return &PCRValues{Values: make(map[int][]byte)}
}

func (p *PCRValues) Set(pcr int, value []byte) {
	p.Values[pcr] = value
}

func (p *PCRValues) Get(pcr int) ([]byte, bool) {
	val, ok := p.Values[pcr]
	return val, ok
}

func (p *PCRValues) PCRDigest(selection *PCRSelection) []byte {
	hash := sha256.New()
	for _, pcr := range selection.PCRs {
		if val, ok := p.Values[pcr]; ok {
			hash.Write(val)
		}
	}
	return hash.Sum(nil)
}

func VerifyPCRValues(values *PCRValues, selection *PCRSelection, expectedDigest [32]byte) error {
	calculatedDigest := values.PCRDigest(selection)
	var digest [32]byte
	copy(digest[:], calculatedDigest)
	if digest != expectedDigest {
		return fmt.Errorf("PCR digest mismatch: expected %x, got %x", expectedDigest, digest)
	}
	return nil
}

type PCRBankInfo struct {
	Bank        int
	HashAlgID   uint16
	HashAlgName string
	NumPCRs     int
}

func GetPCRBankInfo(bank int) (*PCRBankInfo, error) {
	switch bank {
	case PCRBankSHA256:
		return &PCRBankInfo{Bank: PCRBankSHA256, HashAlgID: 0x000B, HashAlgName: "SHA256", NumPCRs: 24}, nil
	case PCRBankSHA1:
		return &PCRBankInfo{Bank: PCRBankSHA1, HashAlgID: 0x0004, HashAlgName: "SHA1", NumPCRs: 24}, nil
	case PCRBankSHA384:
		return &PCRBankInfo{Bank: PCRBankSHA384, HashAlgID: 0x000C, HashAlgName: "SHA384", NumPCRs: 24}, nil
	case PCRBankSM3:
		return &PCRBankInfo{Bank: PCRBankSM3, HashAlgID: 0x0012, HashAlgName: "SM3", NumPCRs: 24}, nil
	default:
		return nil, fmt.Errorf("unknown PCR bank: %d", bank)
	}
}

type PCRQuote struct {
	Selection *PCRSelection
	Digest    [32]byte
	Values    *PCRValues
}

func ValidatePCRQuote(quote *PCRQuote, expectedValues *PCRValues, expectedDigest [32]byte) error {
	if quote == nil {
		return errors.New("quote is nil")
	}
	if quote.Digest != expectedDigest {
		return fmt.Errorf("PCR quote digest mismatch")
	}
	if expectedValues != nil {
		for pcr, expectedVal := range expectedValues.Values {
			actualVal, ok := quote.Values.Get(pcr)
			if !ok {
				return fmt.Errorf("PCR %d not in quote", pcr)
			}
			if string(actualVal) != string(expectedVal) {
				return fmt.Errorf("PCR %d value mismatch", pcr)
			}
		}
	}
	return nil
}

func PCRFingerprint(values *PCRValues, selection *PCRSelection) string {
	digest := values.PCRDigest(selection)
	return fmt.Sprintf("%x", sha256.Sum256(digest))
}

func CommonPCRSelection() *PCRSelection {
	return NewPCRSelection(PCRBankSHA256, []int{PCR0, PCR1, PCR2, PCR3, PCR4, PCR5, PCR6, PCR7})
}

func BootPCRSelection() *PCRSelection {
	return NewPCRSelection(PCRBankSHA256, []int{PCR0, PCR1, PCR2, PCR3, PCR4, PCR5, PCR6, PCR7, PCR8, PCR9, PCR10, PCR11, PCR12})
}

func SecureBootPCRSelection() *PCRSelection {
	return NewPCRSelection(PCRBankSHA256, []int{PCR7, PCR0, PCR4})
}

func ParsePCREvent(eventBytes []byte) (pcrIndex int, eventType uint32, data []byte, digest []byte, err error) {
	if len(eventBytes) < 12 {
		err = errors.New("event too short")
		return
	}
	eventType = binary.BigEndian.Uint32(eventBytes[0:4])
	pcrIndex = int(binary.BigEndian.Uint32(eventBytes[4:8]))
	digestLen := binary.BigEndian.Uint32(eventBytes[8:12])
	if digestLen > 64 || 12+int(digestLen) > len(eventBytes) {
		err = errors.New("invalid digest length")
		return
	}
	digest = eventBytes[12 : 12+int(digestLen)]
	data = eventBytes[12+int(digestLen):]
	return
}
