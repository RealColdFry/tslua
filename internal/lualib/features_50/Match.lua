local function __TS__Match(s, pattern, init)
    local ____temp_0 = {string.find(s, pattern, init)}
    local start = ____temp_0[1]
    local ____end = ____temp_0[2]
    local captures = __TS__ArraySlice(____temp_0, 2)
    if start == nil or ____end == nil then
        return
    elseif table.getn(captures) <= 0 then
        return __TS__StringSlice(s, start - 1, ____end)
    else
        return unpack(captures)
    end
end