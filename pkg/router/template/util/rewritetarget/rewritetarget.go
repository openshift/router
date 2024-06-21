package rewritetarget

import (
	"regexp"
	"strings"
)

var (
	// oneOrMorePercentSigns matches one or more literal percent
	// signs.
	oneOrMorePercentSigns = regexp.MustCompile(`%+`)

	// oneOrMoreBackslashes matches one or more literal
	// backslashes.
	oneOrMoreBackslashes = regexp.MustCompile(`\\+`)
)

type runeResult struct {
	value   string // The rune to append to the result.
	escaped bool   // Whether the next rune is escaped.
	skip    bool   // Whether to skip appending this rune.
	stop    bool   // Whether to stop processing further runes.
}

type processFunc func(char rune, escaped bool) runeResult

// processRunes iterates through the input string and calls processFn
// for each rune. It uses the result of processFn to determine whether
// to continue, skip the rune, or stop processing the input. It returns
// a processed string.
func processRunes(input string, processFn processFunc) string {
	var sb strings.Builder

	// Increase buffer capacity to improve performance by
	// preallocating space for the string being built. The buffer
	// is set to 125% of the input length to accommodate
	// additional characters without the need for further
	// allocation.
	sb.Grow(len(input) + (len(input) / 4))

	// escapeFlag tracks if the next rune is escaped.
	escapeFlag := false

	for _, char := range input {
		result := processFn(char, escapeFlag)
		escapeFlag = result.escaped
		if result.stop {
			break
		}
		if !result.skip {
			sb.WriteString(result.value)
		}
	}

	return sb.String()
}

// replacePercentSigns replaces each % to with %% if the current sequence
// contains an odd number %. Previously, a single % produced a syntax error,
// and users had to use %% to represent a literal %. To ensure compatibility
// while resolving this problem, we must continue to replace single % with
// double %%, but leave double %%  as is, so that existing users who manually
// addressed the issue are not broken. Furthermore, even sequences, such as
// %%%% or %%%%%% were also valid and should be left untouched, while odd
// sequences such as %%% or %%%%% were invalid, and each % should be doubled
// to ensure every % is escaped.
func replacePercentSigns(val string) string {
	return oneOrMorePercentSigns.ReplaceAllStringFunc(val, func(match string) string {
		if len(match)%2 == 1 {
			// For an odd count of %, replace each % with %%.
			return strings.ReplaceAll(match, "%", "%%")
		}
		return match
	})
}

// removeSingleBackslashes removes single backslashes \.
func removeSingleBackslashes(val string) string {
	return oneOrMoreBackslashes.ReplaceAllStringFunc(val, func(match string) string {
		if len(match) == 1 {
			return oneOrMoreBackslashes.ReplaceAllString(match, "")
		}
		return match
	})
}

// convertDoubleBackslashes converts double backslashes \\ to single
// backslashes \.
func convertDoubleBackslashes(val string) string {
	return strings.ReplaceAll(val, `\\`, `\`)
}

// newProcessHashCreator creates a function to process '#' runes in a
// string. It returns a processFunc which will set
// encounteredCommentMarker to true and stop processing if it
// encounters a '#' that is not escaped.
func newProcessHashCreator(encounteredCommentMarker *bool) processFunc {
	return func(char rune, escaped bool) runeResult {
		if char == '#' && !escaped {
			*encounteredCommentMarker = true
			return runeResult{stop: true}
		}
		return runeResult{value: string(char), escaped: char == '\\' && !escaped}
	}
}

// processDoubleQuotes is a processFunc that processes double quote
// characters. It skips the double quote if it's not escaped.
func processDoubleQuotes(char rune, escaped bool) runeResult {
	if char == '"' && !escaped {
		return runeResult{skip: true}
	}
	return runeResult{value: string(char), escaped: char == '\\' && !escaped}
}

// processSingleQuotes is a processFunc that processes single quote
// characters. It skips the single quote if it's not escaped. If it's
// escaped, it adds an escaped single quote for later conversion.
func processSingleQuotes(char rune, escaped bool) runeResult {
	if char == '\'' && !escaped {
		return runeResult{skip: true}
	} else if char == '\'' && escaped {
		// TRICKY: Because our later operation removes single \,
		// we should double escape \\ it, so it will be converted
		// to \ later.
		return runeResult{value: `'\\''`}
	}
	return runeResult{value: string(char), escaped: char == '\\' && !escaped}
}

// SanitizeInput processes the `haproxy.router.openshift.io/rewrite-target`
// annotation value for API compatibility while properly handling values
// with spaces, backslashes, and other special characters. Because the
// annotation value was initially introduced without being enclosed in
// single quotes, certain characters were interpreted rather than being
// treated literally. We later added the single quotes wrapping to resolve
// OCPBUGS-22739. However, we must still maintain API compatibility after
// this change: the annotation values MUST be interpreted to the same values
// after updating to enclose the value in single quotes.
func SanitizeInput(val string) string {
	var encounteredCommentMarker bool

	val = processRunes(val, newProcessHashCreator(&encounteredCommentMarker))
	val = processRunes(val, processDoubleQuotes)
	val = processRunes(val, processSingleQuotes)
	val = replacePercentSigns(val)
	val = removeSingleBackslashes(val)
	val = convertDoubleBackslashes(val)

	if !encounteredCommentMarker {
		// The literal `\1` is appended to annotations without
		// comments to comply with HAProxy rewrite rule
		// expectations, which necessitate a capture group
		// reference (i.e., `\1`) for dynamic substitutions.
		val += `\1`
	}

	return val
}
