package filter

import (
	"fmt"
	"strings"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokInt
	tokString
	tokClass // status class such as 5xx
	tokLParen
	tokRParen
	tokNot
	tokAnd
	tokOr
	tokEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
	tokMatch
	tokNotMatch
)

var tokenNames = map[tokenKind]string{
	tokEOF:      "end of expression",
	tokIdent:    "field",
	tokInt:      "integer",
	tokString:   "string",
	tokClass:    "status class",
	tokLParen:   `"("`,
	tokRParen:   `")"`,
	tokNot:      `"!"`,
	tokAnd:      `"&&"`,
	tokOr:       `"||"`,
	tokEq:       `"=="`,
	tokNe:       `"!="`,
	tokLt:       `"<"`,
	tokLe:       `"<="`,
	tokGt:       `">"`,
	tokGe:       `">="`,
	tokMatch:    `"=~"`,
	tokNotMatch: `"!~"`,
}

func (k tokenKind) String() string { return tokenNames[k] }

// token is a lexical token. Text holds the identifier, the digits, the
// unescaped string contents, or the class digit, depending on Kind.
type token struct {
	Kind tokenKind
	Text string
	Col  int // 1-based column of the first character
}

// Error is a filter error at a specific column of the expression.
type Error struct {
	Expr string // the whole expression, set by Compile
	Col  int    // 1-based
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s at column %d", e.Msg, e.Col)
}

// Caret renders the expression with a caret under the offending column,
// each line indented by two spaces.
func (e *Error) Caret() string {
	return "  " + e.Expr + "\n  " + strings.Repeat(" ", e.Col-1) + "^"
}

func errorf(col int, format string, args ...any) error {
	return &Error{Col: col, Msg: fmt.Sprintf(format, args...)}
}

// twoCharOps maps two-character operators to their kinds. It is checked
// before single-character operators so that "!=" is not lexed as "!".
var twoCharOps = map[string]tokenKind{
	"&&": tokAnd, "||": tokOr,
	"==": tokEq, "!=": tokNe,
	"<=": tokLe, ">=": tokGe,
	"=~": tokMatch, "!~": tokNotMatch,
}

var oneCharOps = map[rune]tokenKind{
	'(': tokLParen, ')': tokRParen,
	'!': tokNot, '<': tokLt, '>': tokGt,
}

// lex splits expr into tokens, ending with a tokEOF token.
func lex(expr string) ([]token, error) {
	src := []rune(expr)
	var toks []token

	for i := 0; i < len(src); {
		c := src[i]
		col := i + 1

		switch {
		case c == ' ' || c == '\t':
			i++

		case isLetter(c):
			start := i
			for i < len(src) && (isLetter(src[i]) || isDigit(src[i])) {
				i++
			}
			toks = append(toks, token{tokIdent, string(src[start:i]), col})

		case isDigit(c):
			start := i
			for i < len(src) && isDigit(src[i]) {
				i++
			}
			end := i
			for i < len(src) && (isLetter(src[i]) || isDigit(src[i])) {
				i++
			}
			word := string(src[start:i])
			switch {
			case i == end:
				toks = append(toks, token{tokInt, word, col})
			case len(word) == 3 && word[1:] == "xx" && word[0] >= '1' && word[0] <= '5':
				toks = append(toks, token{tokClass, word[:1], col})
			default:
				return nil, errorf(col, "invalid number %q", word)
			}

		case c == '"':
			s, n, err := lexString(src[i:], col)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tokString, s, col})
			i += n

		default:
			if i+1 < len(src) {
				if kind, ok := twoCharOps[string(src[i:i+2])]; ok {
					toks = append(toks, token{kind, "", col})
					i += 2
					continue
				}
			}
			if kind, ok := oneCharOps[c]; ok {
				toks = append(toks, token{kind, "", col})
				i++
				continue
			}
			return nil, errorf(col, "unexpected character %q", c)
		}
	}

	return append(toks, token{tokEOF, "", len(src) + 1}), nil
}

// lexString reads a double-quoted string at the start of src and returns its
// unescaped contents and the number of runes consumed. `\"` and `\\` are
// escapes; any other backslash is kept as-is so that regexes like `\d` work.
func lexString(src []rune, col int) (string, int, error) {
	var b strings.Builder
	for i := 1; i < len(src); i++ {
		switch src[i] {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			if i+1 < len(src) && (src[i+1] == '"' || src[i+1] == '\\') {
				i++
			}
		}
		b.WriteRune(src[i])
	}
	return "", 0, errorf(col, "unterminated string")
}

func isLetter(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }
