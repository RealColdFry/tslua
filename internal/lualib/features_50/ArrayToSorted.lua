local function __TS__ArrayToSorted(self, compareFn)
    local copy = {unpack(self)}
    __TS__ArraySort(copy, compareFn)
    return copy
end