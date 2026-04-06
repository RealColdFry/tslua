local function __TS__SparseArrayNew(...)
    local sparseArray = {unpack(arg)}
    sparseArray.sparseLength = __TS__CountVarargs(unpack(arg))
    return sparseArray
end