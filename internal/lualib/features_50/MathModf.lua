local function __TS__MathModf(x)
    local integral = x > 0 and math.floor(x) or math.ceil(x)
    return integral, x - integral
end