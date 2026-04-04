local function __TS__ArrayToReversed(self)
    local copy = {unpack(self)}
    __TS__ArrayReverse(copy)
    return copy
end