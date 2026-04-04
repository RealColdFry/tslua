local function __TS__ArrayConcat(self, ...)
    local items = arg
    local result = {}
    local len = 0
    for i = 1, table.getn(self) do
        len = len + 1
        result[len] = self[i]
    end
    for i = 1, table.getn(items) do
        local item = items[i]
        if __TS__ArrayIsArray(item) then
            for j = 1, table.getn(item) do
                len = len + 1
                result[len] = item[j]
            end
        else
            len = len + 1
            result[len] = item
        end
    end
    return result
end