local function __TS__SparseArrayPush(sparseArray, ...)
    local args = arg
    local argsLen = __TS__CountVarargs(unpack(arg))
    local listLen = sparseArray.sparseLength
    for i = 1, argsLen do
        sparseArray[listLen + i] = args[i]
    end
    sparseArray.sparseLength = listLen + argsLen
end