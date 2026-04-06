local function arrayLikeStep(self, index)
    index = index + 1
    if index > self.length then
        return
    end
    return index, self[index]
end
local function arrayLikeIterator(arr)
    if type(arr.length) == "number" then
        return arrayLikeStep, arr, 0
    end
    return __TS__Iterator(arr)
end
local function __TS__ArrayFrom(arrayLike, mapFn, thisArg)
    local result = {}
    if mapFn == nil then
        for _, v in arrayLikeIterator(arrayLike) do
            result[table.getn(result) + 1] = v
        end
    else
        local i = 0
        for _, v in arrayLikeIterator(arrayLike) do
            local ____result_4 = result
            local ____mapFn_3 = mapFn
            local ____thisArg_1 = thisArg
            local ____v_2 = v
            local ____i_0 = i
            i = ____i_0 + 1
            ____result_4[table.getn(____result_4) + 1] = ____mapFn_3(____thisArg_1, ____v_2, ____i_0)
        end
    end
    return result
end