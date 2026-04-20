package agent

// CountTokens returns the estimated token count for the given text.
// Uses a Unicode-aware approximation for cl100k_base encoding.
// For exact counting, integrate the full tiktoken table.
func CountTokens(text string) int {
	return CountTokensFast(text)
}

// CountTokensFast provides a quick token estimate using byte-level approximation.
// This is an improved estimation that handles both ASCII and non-ASCII text.
func CountTokensFast(text string) int {
	if text == "" {
		return 0
	}

	// Count bytes
	byteLen := len(text)

	// Count non-ASCII bytes
	nonASCII := 0
	for i := 0; i < len(text); i++ {
		if text[i] >= 128 {
			nonASCII++
		}
	}
	ascii := byteLen - nonASCII

	// Approximate: ascii/4 + nonASCII/2 tokens
	// Add base overhead for tokenization overhead
	return (ascii+3)/4 + (nonASCII+1)/2
}

// CountTokensRuneCount counts tokens by rune approximation.
// Each rune is approximately 0.25 tokens for English, more for CJK.
func CountTokensRuneCount(text string) int {
	if text == "" {
		return 0
	}

	runes := 0
	for _, r := range text {
		if r < 128 {
			// ASCII: ~4 chars per token
			runes += 1
		} else {
			// Non-ASCII: ~2 chars per token
			runes += 2
		}
	}
	return (runes + 3) / 4
}