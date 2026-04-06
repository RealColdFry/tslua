local function __TS__ArrayMap(self, callbackfn, thisArg)
    local result = {}
    for i = 1, table.getn(self) do
        result[i] = callbackfn(thisArg, self[i], i - 1, self)
    end
    return result
end