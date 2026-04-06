-- Long-running Lua process that evaluates code snippets.
-- Protocol (length-prefixed):
--   Request:  <byte_count>\n<lua_code>\n
--   Response: OK <byte_count>\n<output>\n
--         or: ERR <byte_count>\n<error_message>\n

-- Lua 5.3+ removed loadstring in favor of load
loadstring = loadstring or load

-- Save references we need before anything can override them
local real_stdout = io.stdout
local real_stdin = io.stdin

-- Built-in modules to preserve across require cache clears
local builtin = {}
for k in pairs(package.loaded) do
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
        real_stdout:write("ERR " .. #msg .. "\n" .. msg .. "\n")
        real_stdout:flush()
        break
    end

    local code = real_stdin:read(len)
    if not code then break end
    -- Consume trailing newline after code
    real_stdin:read("*l")

    -- Clear require cache so each request gets fresh modules
    for k in pairs(package.loaded) do
        if not builtin[k] then
            package.loaded[k] = nil
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
    local function capture(...)
        for i = 1, select("#", ...) do
            output[#output + 1] = tostring(select(i, ...))
        end
    end
    io.write = capture
    -- Override print to capture (print adds tabs between args and newline at end)
    print = function(...)
        local args = {...}
        for i = 1, select("#", ...) do
            if i > 1 then output[#output + 1] = "\t" end
            output[#output + 1] = tostring(args[i])
        end
        output[#output + 1] = "\n"
    end

    local fn, load_err = loadstring(code)
    if fn then
        local ok, run_err = pcall(fn)
        if ok then
            local result = table.concat(output)
            real_stdout:write("OK " .. #result .. "\n" .. result .. "\n")
        else
            local msg = tostring(run_err)
            real_stdout:write("ERR " .. #msg .. "\n" .. msg .. "\n")
        end
    else
        local msg = tostring(load_err)
        real_stdout:write("ERR " .. #msg .. "\n" .. msg .. "\n")
    end
    real_stdout:flush()
end
