local function __TS__StringEndsWith(self, searchString, endPosition)
    if endPosition == nil or endPosition > string.len(self) then
        endPosition = string.len(self)
    end
    return string.sub(
        self,
        endPosition - string.len(searchString) + 1,
        endPosition
    ) == searchString
end