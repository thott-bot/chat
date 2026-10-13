local _, T = ...

local Kit, Format, Store = T.Kit, T.Format, T.Store
local Glance = {}
T.Glance = Glance

local ROW = 28
local SHOWN = 4
local WORD = { needs_you = "Needs you", error = "Stopped", active = "Working", queued = "Working" }

function Glance.Threads()
    local threads = {}
    for _, thread in ipairs(Store.Threads()) do
        if Store.Rank(thread) < 2 or Store.IsUnread(thread) then
            table.insert(threads, thread)
        end
    end
    return threads
end

local function row(parent, onSelect)
    local button = CreateFrame("Button", nil, parent)
    button:SetHeight(ROW)
    button.hover = Kit.Fill(button, "surface-1")
    button.hover:Hide()
    button.mark = button:CreateTexture(nil, "ARTWORK")
    button.mark:SetSize(Kit.space["space-2"], Kit.space["space-2"])
    button.mark:SetPoint("LEFT", Kit.space["space-3"], 0)
    button.dot = button:CreateTexture(nil, "ARTWORK")
    button.dot:SetSize(Kit.space["space-1"], Kit.space["space-1"])
    button.dot:SetPoint("LEFT", Kit.space["space-3"] + 2, 0)
    Kit.Paint(button.dot, "accent")
    button.spinner = Kit.Spinner(button)
    button.spinner:SetPoint("LEFT", Kit.space["space-3"], 0)
    button.word = Kit.Text(button, "t-meta", "ink-3")
    button.word:SetPoint("RIGHT", -Kit.space["space-3"], 0)
    button.title = Kit.Text(button, "t-ui", "ink")
    button.title:SetPoint("LEFT", Kit.space["space-8"], 0)
    button.title:SetPoint("RIGHT", button.word, "LEFT", -Kit.space["space-1"], 0)
    button.title:SetHeight(Kit.space["space-4"])
    button.title:SetJustifyV("MIDDLE")
    button.title:SetWordWrap(false)
    button:SetScript("OnEnter", function(self) self.hover:Show() end)
    button:SetScript("OnLeave", function(self) self.hover:Hide() end)
    button:SetScript("OnClick", function(self) onSelect(self.id) end)
    Kit.Tip(button, button.title)
    return button
end

function Glance.Build(parent, onSelect)
    local glance = CreateFrame("Frame", nil, parent)
    glance.fill = Kit.Fill(glance, "well")
    Kit.Hairline(glance, "bevel-shade", "TOP")
    glance.more = Kit.Text(glance, "t-meta", "ink-3")
    local rows = {}

    function glance:Reveal()
        for index, button in ipairs(rows) do
            if button:IsShown() then
                Kit.Rise(button, Kit.Stagger(index))
            end
        end
    end

    function glance:Render()
        local threads = Glance.Threads()
        local count = math.min(#threads, SHOWN)
        for index = 1, count do
            local thread = threads[index]
            local button = rows[index] or row(self, onSelect)
            rows[index] = button
            button.id = thread.id
            local rank = Store.Rank(thread)
            button.mark:SetShown(rank == 0)
            Kit.Paint(button.mark, thread.state == "error" and "agent-error" or "agent-needs")
            button.spinner:SetShown(rank == 1)
            button.dot:SetShown(rank == 2)
            button.word:SetText(WORD[thread.state] or "Replied")
            button.title:SetText(Format.Escape(Store.Title(thread)))
            button:ClearAllPoints()
            button:SetPoint("TOPLEFT", 0, -(index - 1) * ROW)
            button:SetPoint("RIGHT")
            button:Show()
        end
        for index = count + 1, #rows do
            rows[index]:Hide()
        end
        local height = count * ROW
        glance.more:SetShown(#threads > SHOWN)
        if #threads > SHOWN then
            glance.more:SetText((#threads - SHOWN) .. " more")
            glance.more:ClearAllPoints()
            Kit.Place(glance.more, glance, Kit.space["space-8"], height + Kit.space["space-1"])
            height = height + Kit.space["space-1"] + Kit.TextHeight(glance.more)
        end
        self:SetShown(count > 0)
        if count == 0 then
            return 0
        end
        height = height + Kit.space["space-1"]
        self:SetHeight(height)
        return height
    end

    return glance
end
