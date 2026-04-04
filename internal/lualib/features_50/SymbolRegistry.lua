local symbolRegistry = {}
local function __TS__SymbolRegistryFor(key)
    if not symbolRegistry[key] then
        symbolRegistry[key] = __TS__Symbol(key)
    end
    return symbolRegistry[key]
end
local function __TS__SymbolRegistryKeyFor(sym)
    for key in pairs(symbolRegistry) do
        if symbolRegistry[key] == sym then
            return key
        end
    end
    return nil
end