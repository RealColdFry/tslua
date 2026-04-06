local function __TS__CountVarargs(...)
    local args = arg
    return table.getn(args)
end