-- Long-running Lua 5.0 process that evaluates code snippets.
-- Lua 5.0 lacks: # operator, select(), package table.
-- Uses string.len / table.getn / arg table / _LOADED instead.
-- Protocol (length-prefixed):
--   Request:  <byte_count>\n<lua_code>\n
--   Response: OK <byte_count>\n<output>\n
--         or: ERR <byte_count>\n<error_message>\n

-- Save references we need before anything can override them
local real_stdout = io.stdout
local real_stdin = io.stdin

-- Module cache: Lua 5.0 uses _LOADED, 5.1+ uses package.loaded
local loaded = _LOADED or (package and package.loaded) or {}

-- Built-in modules to preserve across require cache clears
local builtin = {}
for k in pairs(loaded) do
    builtin[k] = true
end

-- Snapshot of the clean global environment
local clean_G = {}
for k, v in pairs(_G) do
    clean_G[k] = v
end

real_stdout:write("READY\n")
real_stdout:flush()

while true do
    local line = real_stdin:read("*l")
    if not line then break end

    local len = tonumber(line)
    if not len then
        local msg = "invalid length: " .. tostring(line)
        real_stdout:write("ERR " .. string.len(msg) .. "\n" .. msg .. "\n")
        real_stdout:flush()
        break
    end

    local code = real_stdin:read(len)
    if not code then break end
    -- Consume trailing newline after code
    real_stdin:read("*l")

    -- Clear require cache so each request gets fresh modules
    for k in pairs(loaded) do
        if not builtin[k] then
            loaded[k] = nil
        end
    end

    -- Restore clean global state (remove any globals set by previous test)
    for k in pairs(_G) do
        if clean_G[k] == nil then
            _G[k] = nil
        end
    end
    for k, v in pairs(clean_G) do
        _G[k] = v
    end

    -- Capture all output (io.write and print) into a buffer
    local output = {}
    io.write = function(...)
        local a = arg
        for i = 1, a.n do
            output[table.getn(output) + 1] = tostring(a[i])
        end
    end
    -- Override print to capture (print adds tabs between args and newline at end)
    print = function(...)
        local a = arg
        for i = 1, a.n do
            if i > 1 then output[table.getn(output) + 1] = "\t" end
            output[table.getn(output) + 1] = tostring(a[i])
        end
        output[table.getn(output) + 1] = "\n"
    end

    local fn, load_err = loadstring(code)
    if fn then
        local ok, run_err = pcall(fn)
        if ok then
            local result = table.concat(output)
            real_stdout:write("OK " .. string.len(result) .. "\n" .. result .. "\n")
        else
            local msg = tostring(run_err)
            real_stdout:write("ERR " .. string.len(msg) .. "\n" .. msg .. "\n")
        end
    else
        local msg = tostring(load_err)
        real_stdout:write("ERR " .. string.len(msg) .. "\n" .. msg .. "\n")
    end
    real_stdout:flush()
end
