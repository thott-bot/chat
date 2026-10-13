local _, T = ...

local Kit = T.Kit
local Composer = {}
T.Composer = Composer

local LINES = 6

function Composer.Build(parent, handlers)
    local frame = CreateFrame("Frame", nil, parent)
    frame.fill = Kit.Fill(frame, "surface-1")
    Kit.Hairline(frame, "edge", "TOP")
    Kit.Hairline(frame, "bevel-light", "TOP", { pixels = 1 })
    local pad = Kit.space["space-3"]
    local body = T.Tokens.font["t-body"]
    local inset = (Kit.size.control + 2 * Kit.space["space-1"] + 2 - body.size) / 2

    local field = Kit.Recess(CreateFrame("Frame", nil, frame))
    field:SetPoint("TOPLEFT", pad, -(1 + pad))
    field:SetPoint("TOPRIGHT", -pad, -(1 + pad))

    local send = Kit.Key(field, nil, true)
    send:SetGlyph("send")
    send:SetPoint("BOTTOMRIGHT", -(Kit.space["space-1"] + 1), Kit.space["space-1"] + 1)
    send:Disable()

    local scroll = CreateFrame("ScrollFrame", nil, field)
    scroll:SetPoint("TOPLEFT", pad + 1, -inset)
    scroll:SetPoint("BOTTOMRIGHT", send, "BOTTOMLEFT", -Kit.space["space-2"], inset - Kit.space["space-1"] - 1)
    local edit = CreateFrame("EditBox", nil, scroll)
    edit:SetMultiLine(true)
    edit:SetAutoFocus(false)
    edit:SetFontObject(Kit.Font("t-body"))
    edit:SetTextColor(Kit.Color("ink"))
    edit:SetWidth(1)
    scroll:SetScrollChild(edit)
    scroll:SetScript("OnSizeChanged", function(_, width) edit:SetWidth(width) end)
    scroll:SetScript("OnMouseDown", function() edit:SetFocus() end)

    local placeholder = Kit.Text(field, "t-body", "ink-3")
    placeholder:SetPoint("TOPLEFT", scroll, "TOPLEFT")

    local context = Kit.Link(frame, "t-meta", "ink-3", "ink-2")
    context:SetPoint("BOTTOMLEFT", pad, Kit.space["space-2"])
    context.mark = context:CreateTexture(nil, "ARTWORK")
    context.mark:SetSize(Kit.space["space-1"], Kit.space["space-1"])
    context.mark:SetPoint("LEFT")
    context.text:SetPoint("LEFT", context.mark, "RIGHT", Kit.space["space-2"], 0)
    context:SetScript("OnClick", handlers.context)

    local choice = Kit.Link(frame, "t-meta", "ink-3", "ink-2", "down")
    choice:SetPoint("BOTTOMRIGHT", -pad, Kit.space["space-2"])
    choice:SetScript("OnClick", handlers.choice)

    local about = Kit.Text(frame, "t-meta", "ink-3")
    about:SetPoint("RIGHT", choice, "RIGHT")
    about:SetWordWrap(false)

    local function fit()
        local left = Kit.space["space-1"] + Kit.space["space-2"] + context.text:GetUnboundedStringWidth()
        local right = choice:IsShown() and choice.text:GetUnboundedStringWidth() + Kit.space["space-1"] + choice.glyph:GetWidth()
            or about:IsShown() and about:GetUnboundedStringWidth() or 0
        local room = math.max(1, frame:GetWidth() - 2 * pad - (right > 0 and Kit.space["space-2"] or 0))
        local fraction = math.min(1, room / math.max(1, left + right))
        context:SetWidth(left * fraction)
        choice:SetWidth(math.max(1, right * fraction))
        about:SetWidth(math.max(1, right * fraction))
    end
    frame:SetScript("OnSizeChanged", fit)

    local refit = false
    local function resize()
        local height = math.max(body.size, math.min(LINES * body.line - (body.line - body.size), edit:GetHeight()))
        field:SetHeight(height + 2 * inset)
        frame:SetHeight(1 + pad + field:GetHeight() + 2 * Kit.space["space-2"] + Kit.space["space-4"])
    end

    local function submit()
        local text = strtrim(edit:GetText())
        if text ~= "" and handlers.send(text) then
            edit:SetText("")
        end
    end

    edit:SetScript("OnTextChanged", function(self)
        ScrollingEdit_OnTextChanged(self, scroll)
        placeholder:SetShown(self:GetText() == "")
        send:SetEnabled(frame.enabled and strtrim(self:GetText()) ~= "")
    end)
    edit:SetScript("OnCursorChanged", ScrollingEdit_OnCursorChanged)
    edit:SetScript("OnUpdate", function(self, elapsed)
        if refit then
            refit = false
            resize()
        end
        ScrollingEdit_OnUpdate(self, elapsed, scroll)
    end)
    edit:SetScript("OnSizeChanged", function() refit = true end)
    edit:SetScript("OnEnterPressed", function(self)
        if IsShiftKeyDown() then
            self:Insert("\n")
        else
            submit()
        end
    end)
    edit:SetScript("OnEscapePressed", edit.ClearFocus)
    edit:SetScript("OnEditFocusGained", function()
        for _, edge in ipairs(field.edges) do
            Kit.Paint(edge, "accent")
        end
    end)
    edit:SetScript("OnEditFocusLost", function()
        for _, edge in ipairs(field.edges) do
            Kit.Paint(edge, "edge")
        end
    end)
    send:SetScript("OnClick", submit)

    function frame:Set(state)
        self.enabled = not state.disabled
        placeholder:SetText(state.placeholder)
        edit:SetEnabled(self.enabled)
        send:SetEnabled(self.enabled and strtrim(edit:GetText()) ~= "")
        context.text:SetText(state.context)
        Kit.Paint(context.mark, state.contextOn and "accent" or "line-strong")
        choice:SetShown(state.choice ~= nil)
        if state.choice then
            choice.text:SetText(state.choice)
        end
        about:SetShown(state.about ~= nil)
        about:SetText(state.about or "")
        fit()
    end

    function frame:Draft(text)
        edit:SetText(text)
        edit:SetFocus()
        edit:SetCursorPosition(#text)
    end

    function frame:Focus()
        edit:SetFocus()
    end

    frame.context, frame.choice, frame.about = context, choice, about
    resize()
    return frame
end
