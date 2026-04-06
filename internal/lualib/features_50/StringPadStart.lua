local function __TS__StringPadStart(self, maxLength, fillString)
    if fillString == nil then
        fillString = " "
    end
    if maxLength ~= maxLength then
        maxLength = 0
    end
    if maxLength == -(1 / 0) or maxLength == 1 / 0 then
        error("Invalid string length", 0)
    end
    if string.len(self) >= maxLength or string.len(fillString) == 0 then
        return self
    end
    maxLength = maxLength - string.len(self)
    if maxLength > string.len(fillString) then
        fillString = fillString .. string.rep(
            fillString,
            math.floor(maxLength / string.len(fillString))
        )
    end
    return string.sub(
        fillString,
        1,
        math.floor(maxLength)
    ) .. self
end