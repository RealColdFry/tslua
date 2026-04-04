local function __TS__ArrayUnshift(self, ...)
    local items = arg
    local numItemsToInsert = table.getn(items)
    if numItemsToInsert == 0 then
        return table.getn(self)
    end
    for i = table.getn(self), 1, -1 do
        self[i + numItemsToInsert] = self[i]
    end
    for i = 1, numItemsToInsert do
        self[i] = items[i]
    end
    return table.getn(self)
end