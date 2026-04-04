local function __TS__NumberIsFinite(value)
    return type(value) == "number" and value == value and value ~= 1 / 0 and value ~= -(1 / 0)
end