local function __TS__ArrayWith(self, index, value)
    local copy = {unpack(self)}
    copy[index + 1] = value
    return copy
end