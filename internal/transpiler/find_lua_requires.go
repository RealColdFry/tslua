package transpiler

// Ported from: src/transpilation/find-lua-requires.ts

// LuaRequire represents a require() call found in Lua source code.
type LuaRequire struct {
	From        int
	To          int
	RequirePath string
}

// FindLuaRequires scans Lua source code for require() calls, properly
// handling strings and comments to avoid false positives.
func FindLuaRequires(lua string) []LuaRequire {
	return findRequires(lua, 0)
}

func findRequires(lua string, offset int) []LuaRequire {
	var result []LuaRequire
	n := len(lua)

	for offset < n {
		c := lua[offset]
		if c == 'r' &&
			(offset == 0 ||
				isLuaWhitespace(lua[offset-1]) ||
				lua[offset-1] == ']' ||
				lua[offset-1] == '(' ||
				lua[offset-1] == '[') {
			if req, end, matched := matchRequire(lua, offset); matched {
				offset = end
				result = append(result, req)
			} else {
				offset = end
			}
		} else if c == '"' || c == '\'' {
			_, offset = readLuaString(lua, offset, c)
		} else if c == '-' && offset+1 < n && lua[offset+1] == '-' {
			offset = skipLuaComment(lua, offset)
		} else {
			offset++
		}
	}

	return result
}

func matchRequire(lua string, offset int) (req LuaRequire, end int, matched bool) {
	start := offset
	n := len(lua)
	keyword := "require"

	for i := 0; i < len(keyword); i++ {
		if offset >= n {
			return LuaRequire{}, offset, false
		}
		if lua[offset] != keyword[i] {
			return LuaRequire{}, offset, false
		}
		offset++
	}

	offset = skipLuaWhitespace(lua, offset)

	hasParentheses := false

	if offset >= n {
		return LuaRequire{}, offset, false
	}

	if lua[offset] == '(' {
		hasParentheses = true
		offset++
		offset = skipLuaWhitespace(lua, offset)
	} else if lua[offset] == '"' || lua[offset] == '\'' {
		// require without parentheses
	} else {
		return LuaRequire{}, offset, false
	}

	if offset >= n || (lua[offset] != '"' && lua[offset] != '\'') {
		return LuaRequire{}, offset, false
	}

	requireString, offsetAfterString := readLuaString(lua, offset, lua[offset])
	offset = offsetAfterString

	if hasParentheses {
		offset = skipLuaWhitespace(lua, offset)
		if offset >= n || lua[offset] != ')' {
			return LuaRequire{}, offset, false
		}
		offset++
	}

	return LuaRequire{From: start, To: offset - 1, RequirePath: requireString}, offset, true
}

func readLuaString(lua string, offset int, delimiter byte) (string, int) {
	// Skip opening delimiter
	offset++

	start := offset
	var result []byte

	escaped := false
	n := len(lua)
	for offset < n && (lua[offset] != delimiter || escaped) {
		if lua[offset] == '\\' && !escaped {
			escaped = true
		} else {
			if lua[offset] == delimiter {
				result = append(result, lua[start:offset-1]...)
				start = offset
			}
			escaped = false
		}
		offset++
	}

	result = append(result, lua[start:offset]...)

	// Skip closing delimiter if present
	if offset < n {
		offset++
	}

	return string(result), offset
}

func skipLuaWhitespace(lua string, offset int) int {
	n := len(lua)
	for offset < n && isLuaWhitespace(lua[offset]) {
		offset++
	}
	return offset
}

func isLuaWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func skipLuaComment(lua string, offset int) int {
	// Skip past "--"
	offset += 2

	n := len(lua)
	if offset+1 < n && lua[offset] == '[' && lua[offset+1] == '[' {
		return skipMultiLineComment(lua, offset)
	}
	return skipSingleLineComment(lua, offset)
}

func skipMultiLineComment(lua string, offset int) int {
	n := len(lua)
	for offset < n && !(lua[offset] == ']' && offset > 0 && lua[offset-1] == ']') {
		offset++
	}
	return offset + 1
}

func skipSingleLineComment(lua string, offset int) int {
	n := len(lua)
	for offset < n && lua[offset] != '\n' {
		offset++
	}
	return offset + 1
}
