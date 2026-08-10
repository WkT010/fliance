package wallet

import "testing"

func TestValidateBTCAddress(t *testing.T) {
	valid := []string{
		"1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2",                             // mainnet P2PKH
		"3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy",                             // mainnet P2SH
		"bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",                     // mainnet P2WPKH
		"bc1qrp33g0q5c5txsp9arysrx4k6zdkfs4nce4xj0gdcccefvpysxf3qccfmv3", // mainnet P2WSH
		"mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn",                             // testnet P2PKH
		"tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx",                     // testnet P2WPKH
	}
	for _, addr := range valid {
		if !ValidateBTCAddress(addr) {
			t.Errorf("ValidateBTCAddress(%q) = false, want true", addr)
		}
	}

	invalid := []string{
		"",
		"not-an-address",
		"1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN3", // corrupted checksum
		"1BvBMSEYstWetqTFn5Au4m4GFg7xJaNV",   // too short
		"bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t5", // corrupted bech32 checksum
		"bc1Qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", // mixed case bech32
		"tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", // program checksum valid for bc1, not tb1
		"BTC_deadbeef",
	}
	for _, addr := range invalid {
		if ValidateBTCAddress(addr) {
			t.Errorf("ValidateBTCAddress(%q) = true, want false", addr)
		}
	}
}

func TestValidateETHAddress(t *testing.T) {
	valid := []string{
		// EIP-55 official test vectors (mixed case, checksummed).
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
		// All-lowercase / all-uppercase are checksum-free and accepted.
		"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
		"0x5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED",
	}
	for _, addr := range valid {
		if !ValidateETHAddress(addr) {
			t.Errorf("ValidateETHAddress(%q) = false, want true", addr)
		}
	}

	invalid := []string{
		"",
		"5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",    // missing 0x
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAe",   // 39 hex chars
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed0", // 41 hex chars
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeg",  // non-hex char
		"0x5AAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",  // mixed case, broken checksum
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDB",  // valid vector with case tampered
	}
	for _, addr := range invalid {
		if ValidateETHAddress(addr) {
			t.Errorf("ValidateETHAddress(%q) = true, want false", addr)
		}
	}
}

func TestValidateWithdrawalAddressDispatch(t *testing.T) {
	if err := ValidateWithdrawalAddress("BTC", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"); err != nil {
		t.Errorf("valid BTC rejected: %v", err)
	}
	if err := ValidateWithdrawalAddress("btc", "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"); err != nil {
		t.Errorf("asset case-insensitivity broken: %v", err)
	}
	if err := ValidateWithdrawalAddress("BTC", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"); err == nil {
		t.Error("ETH address accepted as BTC")
	}
	if err := ValidateWithdrawalAddress("ETH", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"); err != nil {
		t.Errorf("valid ETH rejected: %v", err)
	}
	if err := ValidateWithdrawalAddress("POLYGON", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"); err != nil {
		t.Errorf("valid POLYGON rejected: %v", err)
	}
	if err := ValidateWithdrawalAddress("ETH", "0xnothex000000000000000000000000000000000000"); err == nil {
		t.Error("malformed ETH accepted")
	}
	// Unknown asset: no strict format, caller falls back to client check.
	if err := ValidateWithdrawalAddress("SOL", "anything"); err != nil {
		t.Errorf("unknown asset should fall back to client check: %v", err)
	}
}

func TestMockClientDelegatesToStrictValidation(t *testing.T) {
	m := &MockBlockchainClient{Asset: "BTC"} // bypass constructor log in tests
	if m.IsValidAddress("garbage") {
		t.Error("mock accepted malformed BTC address")
	}
	if !m.IsValidAddress("1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2") {
		t.Error("mock rejected valid BTC address")
	}
	e := &MockBlockchainClient{Asset: "ETH"}
	if e.IsValidAddress("0x5AAeb6053F3E94C9b9A09f33669435E7Ef1BeAed") {
		t.Error("mock accepted mixed-case address with broken EIP-55 checksum")
	}
	if !e.IsValidAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed") {
		t.Error("mock rejected correctly checksummed ETH address")
	}
}
