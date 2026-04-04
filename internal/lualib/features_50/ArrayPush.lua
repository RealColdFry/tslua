local function __TS__ArrayPush(self, ...)
    local items = arg
    local len = table.getn(self)
    for i = 1, table.getn(items) do
        len = len + 1
        self[len] = items[i]
    end
    return len
end