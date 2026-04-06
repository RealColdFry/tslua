-- Long-running Luau process (via Lune) that evaluates code snippets.
-- Protocol (length-prefixed), same as lua_eval_server.lua:
--   Request:  <byte_count>\n<lua_code>\n
--   Response: OK <byte_count>\n<output>\n
--         or: ERR <byte_count>\n<error_message>\n

local stdio = require("@lune/stdio")
local luauLib = require("@lune/luau")
local fs = require("@lune/fs")

-- Snapshot of the clean global environment
local clean_G = {}
for k, v in pairs(_G) do
	clean_G[k] = v
end

stdio.write("READY\n")

while true do
	local line = stdio.readLine()
	if line == nil or line == "" then break end

	local len = tonumber(line)
	if not len then
		local msg = "invalid length: " .. tostring(line)
		stdio.write("ERR " .. #msg .. "\n" .. msg .. "\n")
		break
	end

	-- Read exactly `len` bytes of code.
	-- Lune's readLine() includes the trailing \n in the returned string.
	-- Go sends: len\n + code + \n (trailing separator).
	local parts = {}
	local remaining = len
	local consumedTrailing = false
	while remaining > 0 do
		local chunk = stdio.readLine()
		if chunk == nil then break end
		if remaining >= #chunk then
			table.insert(parts, chunk)
			remaining -= #chunk
		else
			-- Took a substring: the rest of the chunk (including Go's trailing \n) is discarded
			table.insert(parts, string.sub(chunk, 1, remaining))
			remaining = 0
			consumedTrailing = true
		end
	end
	-- If we consumed code exactly (no overshoot), Go's trailing \n is still in the stream
	if not consumedTrailing then
		stdio.readLine()
	end
	local code = table.concat(parts)

	-- Build a fresh environment for each eval with captured output
	local output = {}
	local env = {}
	for k, v in pairs(clean_G) do
		env[k] = v
	end

	env.print = function(...)
		local args = { ... }
		for i = 1, select("#", ...) do
			if i > 1 then table.insert(output, "\t") end
			table.insert(output, tostring(args[i]))
		end
		table.insert(output, "\n")
	end

	-- io.write shim (no trailing newline, unlike print)
	env.io = {
		write = function(...)
			for i = 1, select("#", ...) do
				table.insert(output, tostring(select(i, ...)))
			end
		end,
	}

	-- package.path + require shim: runner code sets package.path, require searches it.
	-- The table is shared so mutations from inside luau.load are visible.
	local moduleCache = {}
	env.package = { path = "", loaded = {} }

	env.require = function(modName: string)
		if moduleCache[modName] then
			return moduleCache[modName]
		end
		-- Search package.path patterns (semicolon-separated, ? replaced by modName)
		for pattern in string.gmatch(env.package.path, "[^;]+") do
			local filePath = string.gsub(pattern, "%?", modName)
			local fileOk, src = pcall(fs.readFile, filePath)
			if fileOk then
				local fn = luauLib.load(luauLib.compile(src), { environment = env })
				local result = fn()
				moduleCache[modName] = result
				return result
			end
		end
		error("module '" .. modName .. "' not found in: " .. env.package.path)
	end

	local ok_compile, compiled = pcall(luauLib.compile, code)
	if not ok_compile then
		local msg = tostring(compiled)
		stdio.write("ERR " .. #msg .. "\n" .. msg .. "\n")
		continue
	end

	local ok_load, fn = pcall(luauLib.load, compiled, { environment = env })
	if not ok_load then
		local msg = tostring(fn)
		stdio.write("ERR " .. #msg .. "\n" .. msg .. "\n")
		continue
	end

	local ok, run_err = pcall(fn)
	if ok then
		local result = table.concat(output)
		stdio.write("OK " .. #result .. "\n" .. result .. "\n")
	else
		local msg = tostring(run_err)
		stdio.write("ERR " .. #msg .. "\n" .. msg .. "\n")
	end
end
