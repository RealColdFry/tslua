local function serialize(v)
	local t = type(v)
	if v == nil then return "nil"
	elseif t == "boolean" then return tostring(v)
	elseif t == "number" then
		if v ~= v then return "NaN"
		elseif v == math.huge then return "Infinity"
		elseif v == -math.huge then return "-Infinity"
		elseif v == math.floor(v) and v >= -2^53 and v <= 2^53 then
			return string.format("%.0f", v)
		else
			for p = 14, 17 do
				local s = string.format("%." .. p .. "g", v)
				if tonumber(s) == v then return s end
			end
			return string.format("%.17g", v)
		end
	elseif t == "string" then
		local s = string.format("%q", v)
		s = s:gsub("\\\n", "\\n")
		s = s:gsub("\t", "\\t")
		s = s:gsub("\\(%d)([^%d])", "\\00%1%2")
		s = s:gsub("\\(%d%d)([^%d])", "\\0%1%2")
		s = s:gsub("\\009", "\\t")
		s = s:gsub("\\012", "\\f")
		s = s:gsub("\\013", "\\r")
		return s
	elseif t == "table" then
		local n = #v
		local isArray = true
		if n == 0 then
			isArray = false
		else
			for i = 1, n do
				if v[i] == nil then isArray = false; break end
			end
		end
		if isArray then
			local parts = {}
			for i = 1, n do parts[i] = serialize(v[i]) end
			return "{" .. table.concat(parts, ", ") .. "}"
		else
			local parts = {}
			for k, val in pairs(v) do
				parts[#parts+1] = tostring(k) .. " = " .. serialize(val)
			end
			table.sort(parts)
			return "{" .. table.concat(parts, ", ") .. "}"
		end
	else return tostring(v) end
end
