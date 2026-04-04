local function __TS__Unpack(list, i, j)
    if i == 1 and j == nil then
        return unpack(list)
    else
        if j == nil then
            j = table.getn(list)
        end
        local slice = {}
        do
            local n = i
            while n <= j do
                slice[n - i + 1] = list[n]
                n = n + 1
            end
        end
        return unpack(slice)
    end
end