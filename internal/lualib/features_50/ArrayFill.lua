local function __TS__ArrayFill(self, value, start, ____end)
    local relativeStart = start or 0
    local relativeEnd = ____end or table.getn(self)
    if relativeStart < 0 then
        relativeStart = relativeStart + table.getn(self)
    end
    if relativeEnd < 0 then
        relativeEnd = relativeEnd + table.getn(self)
    end
    do
        local i = relativeStart
        while i < relativeEnd do
            self[i + 1] = value
            i = i + 1
        end
    end
    return self
end