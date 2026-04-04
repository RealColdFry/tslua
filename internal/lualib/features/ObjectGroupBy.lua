local function __TS__ObjectGroupBy(items, keySelector)
    local result = {}
    local i = 0
    for ____, item in __TS__Iterator(items) do
        local key = keySelector(nil, item, i)
        if result[key] ~= nil then
            result[key][#result[key] + 1] = item
        else
            result[key] = {item}
        end
        i = i + 1
    end
    return result
end