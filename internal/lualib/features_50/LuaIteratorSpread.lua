local function __TS__LuaIteratorSpread(self, state, firstKey)
    local results = {}
    local key, value = self(state, firstKey)
    while key do
        results[table.getn(results) + 1] = {key, value}
        key, value = self(state, key)
    end
    return unpack(results)
end