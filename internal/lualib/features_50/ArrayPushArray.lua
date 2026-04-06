local function __TS__ArrayPushArray(self, items)
    local len = table.getn(self)
    for i = 1, table.getn(items) do
        len = len + 1
        self[len] = items[i]
    end
    return len
end