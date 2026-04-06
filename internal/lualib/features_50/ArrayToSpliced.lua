local function __TS__ArrayToSpliced(self, start, deleteCount, ...)
    local copy = {unpack(self)}
    __TS__ArraySplice(
        copy,
        start,
        deleteCount,
        unpack(arg)
    )
    return copy
end