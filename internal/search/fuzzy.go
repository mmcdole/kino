package search

import (
	"strings"
	"unicode"
)

// FuzzyMatch represents a search match result
type FuzzyMatch struct {
	Index          int   // Index in source slice
	Score          int   // Match score (lower = better)
	MatchedIndexes []int // Character positions that matched (for highlighting)
}

// FuzzySearch performs token-based fuzzy matching optimized for media titles.
//
// Algorithm:
//  1. Tokenize query into words
//  2. For each query token, find the best match in the title
//  3. All query tokens must match (AND semantics)
//  4. Word order does not matter ("robot mr" matches "Mr. Robot")
//  5. Typo tolerance based on token length
//
// Returns matches sorted by score (lower = better).
func FuzzySearch(query string, titles []string) []FuzzyMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	var matches []FuzzyMatch

	for i, title := range titles {
		if match, ok := matchTitle(title, queryTokens, i); ok {
			matches = append(matches, match)
		}
	}

	sortMatches(matches, titles)
	return matches
}

// Token represents a word and its position in the original string
type Token struct {
	Text  string // Lowercase text
	Start int    // Start position in original string
	End   int    // End position (exclusive) in original string
}

// tokenize splits text into word tokens, tracking positions
func tokenize(text string) []Token {
	var tokens []Token
	runes := []rune(strings.ToLower(text))

	inWord := false
	wordStart := 0

	for i, r := range runes {
		isWordChar := unicode.IsLetter(r) || unicode.IsDigit(r)

		if isWordChar && !inWord {
			// Starting a new word
			wordStart = i
			inWord = true
		} else if !isWordChar && inWord {
			// Ending a word
			tokens = append(tokens, Token{
				Text:  string(runes[wordStart:i]),
				Start: wordStart,
				End:   i,
			})
			inWord = false
		}
	}

	// Don't forget the last word if text doesn't end with separator
	if inWord {
		tokens = append(tokens, Token{
			Text:  string(runes[wordStart:]),
			Start: wordStart,
			End:   len(runes),
		})
	}

	return tokens
}

// TokenMatch represents how a query token matched a title
type TokenMatch struct {
	Score          int   // Match quality (lower = better)
	MatchedIndexes []int // Character positions in title that matched
}

// matchTitle attempts to match all query tokens against the title
func matchTitle(title string, queryTokens []Token, index int) (FuzzyMatch, bool) {
	lowerTitle := strings.ToLower(title)
	titleTokens := tokenize(title)

	// Track which title tokens have been used (each can only match one query token)
	usedTitleTokens := make([]bool, len(titleTokens))

	var allMatchedIndexes []int
	totalScore := 0

	// Each query token must find a match
	for _, queryToken := range queryTokens {
		bestMatch, bestTitleIdx := findBestTokenMatch(queryToken, titleTokens, lowerTitle, usedTitleTokens)

		if bestMatch.Score < 0 {
			// No match found for this query token
			return FuzzyMatch{}, false
		}

		// Mark this title token as used
		if bestTitleIdx >= 0 {
			usedTitleTokens[bestTitleIdx] = true
		}

		totalScore += bestMatch.Score
		allMatchedIndexes = append(allMatchedIndexes, bestMatch.MatchedIndexes...)
	}

	// Bonus for matching more of the title (penalize titles with many extra words)
	extraWords := len(titleTokens) - len(queryTokens)
	if extraWords > 0 {
		totalScore += extraWords * 5
	}

	return FuzzyMatch{
		Index:          index,
		Score:          totalScore,
		MatchedIndexes: dedupeAndSort(allMatchedIndexes),
	}, true
}

// findBestTokenMatch finds the best matching title token for a query token
func findBestTokenMatch(queryToken Token, titleTokens []Token, lowerTitle string, usedTitleTokens []bool) (TokenMatch, int) {
	bestMatch := TokenMatch{Score: -1}
	bestTitleIdx := -1

	// First, try to match against title tokens (word-level matching)
	for i, titleToken := range titleTokens {
		if usedTitleTokens[i] {
			continue
		}

		match := matchTokenToToken(queryToken.Text, titleToken)
		if match.Score >= 0 && (bestMatch.Score < 0 || match.Score < bestMatch.Score) {
			bestMatch = match
			bestTitleIdx = i
		}
	}

	// If no word-level match, try substring match anywhere in title
	if bestMatch.Score < 0 {
		if match := matchSubstring(queryToken.Text, lowerTitle); match.Score >= 0 {
			bestMatch = match
			bestTitleIdx = -1 // Not a specific token match
		}
	}

	return bestMatch, bestTitleIdx
}

// matchTokenToToken matches a query token against a title token
// Returns score < 0 if no match
func matchTokenToToken(query string, titleToken Token) TokenMatch {
	title := titleToken.Text

	// Exact match (best)
	if query == title {
		indexes := makeIndexRange(titleToken.Start, titleToken.End)
		return TokenMatch{Score: 0, MatchedIndexes: indexes}
	}

	// Prefix match (very good)
	if strings.HasPrefix(title, query) {
		indexes := makeIndexRange(titleToken.Start, titleToken.Start+len([]rune(query)))
		return TokenMatch{Score: 10, MatchedIndexes: indexes}
	}

	// Title token starts with query (query is prefix)
	if strings.HasPrefix(query, title) {
		// Query is longer than title token - partial match
		indexes := makeIndexRange(titleToken.Start, titleToken.End)
		return TokenMatch{Score: 20, MatchedIndexes: indexes}
	}

	// Substring match within the token
	if idx := strings.Index(title, query); idx >= 0 {
		start := titleToken.Start + idx
		indexes := makeIndexRange(start, start+len([]rune(query)))
		return TokenMatch{Score: 50 + idx, MatchedIndexes: indexes}
	}

	// Fuzzy match with typo tolerance
	maxTypos := allowedTypos(len([]rune(query)))
	if maxTypos > 0 {
		dist, indexes := levenshteinWithPositions(query, title, titleToken.Start)
		if dist <= maxTypos {
			return TokenMatch{Score: 100 + dist*20, MatchedIndexes: indexes}
		}
	}

	return TokenMatch{Score: -1}
}

// matchSubstring finds query as a substring anywhere in the title
func matchSubstring(query string, lowerTitle string) TokenMatch {
	if idx := strings.Index(lowerTitle, query); idx >= 0 {
		// Convert byte index to rune index
		runeIdx := len([]rune(lowerTitle[:idx]))
		indexes := makeIndexRange(runeIdx, runeIdx+len([]rune(query)))
		return TokenMatch{Score: 150 + runeIdx, MatchedIndexes: indexes}
	}
	return TokenMatch{Score: -1}
}

// allowedTypos returns the number of typos allowed based on word length
// Industry standard: 1-3 chars = 0, 4-6 chars = 1, 7+ chars = 2
func allowedTypos(length int) int {
	switch {
	case length <= 3:
		return 0
	case length <= 6:
		return 1
	default:
		return 2
	}
}

// levenshteinWithPositions calculates Levenshtein distance and returns matched positions
func levenshteinWithPositions(query, title string, titleOffset int) (int, []int) {
	qRunes := []rune(query)
	tRunes := []rune(title)

	qLen := len(qRunes)
	tLen := len(tRunes)

	if qLen == 0 {
		return tLen, nil
	}
	if tLen == 0 {
		return qLen, nil
	}

	// Create distance matrix
	matrix := make([][]int, qLen+1)
	for i := range matrix {
		matrix[i] = make([]int, tLen+1)
		matrix[i][0] = i
	}
	for j := 0; j <= tLen; j++ {
		matrix[0][j] = j
	}

	// Fill the matrix
	for i := 1; i <= qLen; i++ {
		for j := 1; j <= tLen; j++ {
			cost := 1
			if qRunes[i-1] == tRunes[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	distance := matrix[qLen][tLen]

	// Backtrack to find matched positions
	var matched []int
	i, j := qLen, tLen
	for i > 0 && j > 0 {
		if qRunes[i-1] == tRunes[j-1] {
			matched = append([]int{titleOffset + j - 1}, matched...)
			i--
			j--
		} else if matrix[i-1][j-1] <= matrix[i-1][j] && matrix[i-1][j-1] <= matrix[i][j-1] {
			// Substitution - still mark the position as "matched" for highlighting
			matched = append([]int{titleOffset + j - 1}, matched...)
			i--
			j--
		} else if matrix[i-1][j] < matrix[i][j-1] {
			// Deletion from query
			i--
		} else {
			// Insertion into query
			j--
		}
	}

	return distance, matched
}

// makeIndexRange creates a slice of consecutive integers [start, end)
func makeIndexRange(start, end int) []int {
	indexes := make([]int, end-start)
	for i := range indexes {
		indexes[i] = start + i
	}
	return indexes
}

// dedupeAndSort removes duplicates and sorts indexes
func dedupeAndSort(indexes []int) []int {
	if len(indexes) == 0 {
		return indexes
	}

	seen := make(map[int]bool)
	var result []int
	for _, idx := range indexes {
		if !seen[idx] {
			seen[idx] = true
			result = append(result, idx)
		}
	}

	// Simple insertion sort
	for i := 1; i < len(result); i++ {
		j := i
		for j > 0 && result[j] < result[j-1] {
			result[j], result[j-1] = result[j-1], result[j]
			j--
		}
	}

	return result
}

// sortMatches sorts by score (lower = better), then by title length
func sortMatches(matches []FuzzyMatch, titles []string) {
	for i := 1; i < len(matches); i++ {
		j := i
		for j > 0 && compareFuzzyMatches(matches[j], matches[j-1], titles) {
			matches[j], matches[j-1] = matches[j-1], matches[j]
			j--
		}
	}
}

func compareFuzzyMatches(a, b FuzzyMatch, titles []string) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return len(titles[a.Index]) < len(titles[b.Index])
}
