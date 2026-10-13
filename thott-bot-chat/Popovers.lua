local _, T = ...

local Kit, Format, Store, Actions = T.Kit, T.Format, T.Store, T.Actions
local Popovers = {}
T.Popovers = Popovers

local ONOFF = { { value = false, label = "Off" }, { value = true, label = "On" } }
local SCALE, OPACITY = { 75, 150 }, { 50, 100 }
local APPS = { { value = "codex", label = "Codex" }, { value = "claude", label = "Claude Code" } }

local function stepped(value, range)
    return math.min(range[2], math.max(range[1], math.floor(value * 20 + 0.5) * 5))
end

function Popovers.Settle()
    T.settings.scale = stepped(T.settings.scale, SCALE) / 100
    T.settings.opacity = stepped(T.settings.opacity, OPACITY) / 100
end

local function setting(parent, label, control, y)
    local text = Kit.Text(parent, "t-ui", "ink")
    text:SetPoint("TOPLEFT", Kit.space["space-3"], -y)
    text:SetHeight(Kit.size.control)
    text:SetJustifyV("MIDDLE")
    text:SetText(label)
    control:SetPoint("TOPRIGHT", -Kit.space["space-3"], -y)
    control:SetWidth(160)
    return text
end

local function stagger(rows)
    for index, regions in ipairs(rows) do
        for _, region in ipairs(regions) do
            if region:IsShown() then
                Kit.Rise(region, Kit.Stagger(index))
            end
        end
    end
end

function Popovers.Settings(owner, handlers)
    local popover = Kit.Popover(owner)
    popover:SetPoint("TOPRIGHT", owner, "BOTTOMRIGHT", 0, -Kit.space["space-1"])
    local function slider(key, range, apply, live)
        local control = Kit.Slider(popover, range[1], range[2], 5, function(value)
            T.settings[key] = value / 100
            apply()
        end, live)
        return control, function() control:SetValue(stepped(T.settings[key], range)) end
    end
    local function switch(key, apply)
        local control = Kit.Segmented(popover, ONOFF, function(value)
            T.settings[key] = value
            if apply then
                apply()
            end
        end)
        return control, function() control:SetValue(T.settings[key]) end
    end
    local controls = {
        { "Scale", slider("scale", SCALE, handlers.scale) },
        { "Opacity", slider("opacity", OPACITY, handlers.opacity, true) },
        false,
        { "Alert in chat", switch("alertChat") },
        { "Alert sound", switch("alertSound") },
        { "Minimap button", switch("minimap", T.Alerts.Refresh) },
    }
    local y, widest, syncs, rows = Kit.space["space-2"], 0, {}, {}
    for _, spec in ipairs(controls) do
        if spec then
            local text = setting(popover, spec[1], spec[2], y)
            widest = math.max(widest, text:GetUnboundedStringWidth())
            table.insert(syncs, spec[3])
            table.insert(rows, { text, spec[2] })
            y = y + Kit.size.control + Kit.space["space-1"]
        else
            Kit.Hairline(popover, "line", "TOP", { offset = y + Kit.space["space-1"], indent = Kit.space["space-3"] })
            y = y + 2 * Kit.space["space-1"] + 1
        end
    end
    popover:SetSize(widest + 160 + 3 * Kit.space["space-3"], y + Kit.space["space-2"])
    function popover:Toggle()
        if self:IsShown() then
            Kit.Leave(self)
        else
            for _, sync in ipairs(syncs) do
                sync()
            end
            Kit.Rise(self)
            stagger(rows)
        end
    end
    popover:HookScript("OnShow", function() owner:SetHeld(true) end)
    popover:HookScript("OnHide", function() owner:SetHeld(false) end)
    return popover
end

function Popovers.Parts(owner, handlers)
    local width = 248
    local popover = Kit.Popover(owner)
    popover:SetPoint("BOTTOMLEFT", owner, "TOPLEFT", -Kit.space["space-3"], Kit.space["space-1"])
    local heading = Kit.Text(popover, "t-ui-strong", "ink")
    heading:SetPoint("TOPLEFT", Kit.space["space-3"], -Kit.space["space-2"])
    heading:SetHeight(Kit.size.control)
    heading:SetJustifyV("MIDDLE")
    heading:SetText("Sent with every message")
    local switch = Kit.Segmented(popover, ONOFF, function(value)
        T.settings.extras = value
        handlers.refresh()
    end)
    switch:SetPoint("TOPRIGHT", -Kit.space["space-3"], -Kit.space["space-2"])
    switch:SetWidth(80)
    local y = Kit.space["space-2"] + Kit.size.control + Kit.space["space-1"]
    local parts, rows = {}, { { heading, switch } }
    for _, part in ipairs(T.Context.parts) do
        local button = Kit.Option(popover, function()
            T.settings.context[part.key] = not T.settings.context[part.key]
            handlers.refresh()
        end)
        button:SetPoint("TOPLEFT", Kit.space["space-3"], -y)
        button:SetPoint("RIGHT", -Kit.space["space-3"], 0)
        button.part = part
        table.insert(parts, button)
        table.insert(rows, { button })
        y = y + Kit.size.control
    end
    local show = Kit.Key(popover, "Preview")
    show:SetPoint("TOPLEFT", Kit.space["space-3"], -(y + Kit.space["space-2"]))
    show:SetScript("OnClick", function()
        popover:Hide()
        handlers.preview()
    end)
    local all = Kit.Link(popover, "t-meta", "ink-3", "ink-2")
    all:SetPoint("RIGHT", -Kit.space["space-3"], 0)
    all:SetPoint("TOP", show, "TOP")
    all:SetHeight(Kit.size.control)
    all:SetScript("OnClick", function(self)
        for _, part in ipairs(T.Context.parts) do
            T.settings.context[part.key] = self.on
        end
        handlers.refresh()
    end)
    table.insert(rows, { show, all })
    popover:SetSize(width, y + Kit.space["space-2"] + Kit.size.control + Kit.space["space-3"])
    function popover:Refresh()
        local extras, any = T.settings.extras, false
        switch:SetValue(extras)
        for _, button in ipairs(parts) do
            local on = T.settings.context[button.part.key]
            button:Set(button.part.label, on, extras)
            any = any or on
        end
        show:SetEnabled(extras)
        all:SetShown(extras)
        all.on = not any
        all:SetLabel(any and "None" or "All")
    end
    function popover:Toggle()
        if self:IsShown() then
            Kit.Leave(self)
        else
            self:Refresh()
            Kit.Rise(self)
            stagger(rows)
        end
    end
    return popover
end

local function effortLabel(effort)
    return effort == "xhigh" and "Extra high" or (effort:gsub("^%l", string.upper))
end

local function lead(agent)
    local fallback
    for _, model in ipairs(Store.models) do
        if Actions.Agent(model) == agent then
            if model.default then
                return model
            end
            fallback = fallback or model
        end
    end
    return fallback
end

function Popovers.Picker(owner)
    local inset = Kit.space["space-3"]
    local width = 280
    local column = (width - 3 * inset) / 2
    local popover = Kit.Popover(owner)
    popover:SetPoint("BOTTOMRIGHT", owner, "TOPRIGHT", inset, Kit.space["space-1"])
    popover:SetWidth(width)
    popover.rows = {}
    popover.app = Kit.Segmented(popover, APPS, function(agent)
        local model = lead(agent)
        if model then
            Actions.Choose("model", model.id)
        end
        popover:Fill()
    end)
    popover.app:SetPoint("TOPLEFT", inset, -Kit.space["space-2"])
    popover.app:SetPoint("RIGHT", -inset, 0)
    local top = Kit.space["space-2"] + Kit.size.control + Kit.space["space-2"]
    Kit.Label(popover, "Model"):SetPoint("TOPLEFT", inset, -top)
    Kit.Label(popover, "Effort"):SetPoint("TOPLEFT", 2 * inset + column, -top)
    popover.project = Kit.Label(popover, "Project")
    local index = 0
    local function option(label, on, onClick, x, y, size)
        index = index + 1
        local button = popover.rows[index] or Kit.Option(popover, function(self)
            self.choose()
            popover:Fill()
        end)
        popover.rows[index] = button
        button:ClearAllPoints()
        button:SetPoint("TOPLEFT", x, -y)
        button:SetWidth(size)
        button:Set(Format.Escape(label), on)
        button.choose = onClick
        button:Show()
        return y + Kit.size.control
    end
    function popover:Fill()
        local pick = Actions.Choice()
        local agent = pick.model and Actions.Agent(pick.model) or "codex"
        for _, app in ipairs(APPS) do
            self.app:SetOptionEnabled(app.value, lead(app.value) ~= nil)
        end
        self.app:SetValue(agent)
        index = 0
        local left, right = top + Kit.size.control, top + Kit.size.control
        for _, model in ipairs(Store.models) do
            if Actions.Agent(model) == agent then
                left = option(model.label, model == pick.model, function() Actions.Choose("model", model.id) end, inset, left, column)
            end
        end
        for _, effort in ipairs(pick.model and pick.model.efforts or {}) do
            right = option(effortLabel(effort), effort == pick.effort, function() Actions.Choose("effort", effort) end, 2 * inset + column, right, column)
        end
        local y = math.max(left, right) + Kit.space["space-2"]
        self.project:SetPoint("TOPLEFT", inset, -y)
        y = y + Kit.size.control
        for _, project in ipairs(Store.projects) do
            y = option(project.label, project == pick.project, function() Actions.Choose("cwd", project.cwd) end, inset, y, width - 2 * inset)
        end
        for extra = index + 1, #self.rows do
            self.rows[extra]:Hide()
        end
        self:SetHeight(y + Kit.space["space-3"])
    end
    function popover:Toggle()
        if self:IsShown() then
            Kit.Leave(self)
        else
            self:Fill()
            Kit.Rise(self)
            local rows = { { self.app } }
            for _, button in ipairs(self.rows) do
                table.insert(rows, { button })
            end
            stagger(rows)
        end
    end
    return popover
end

local function sheet(text)
    local lines, held = {}, nil
    for line in text:gmatch("[^\n]+") do
        local key, value = line:match("^(%S[^:]*):%s?(.*)$")
        if key and value == "" then
            held = key
        else
            table.insert(lines, { key = key or held, first = key ~= nil or held ~= nil, value = key and value or strtrim(line) })
            held = nil
        end
    end
    return lines
end

function Popovers.Preview(parent, anchor)
    local preview = Kit.Panel(CreateFrame("Frame", nil, parent), "surface-1")
    preview:SetPoint("TOPLEFT", anchor, "TOPLEFT", Kit.space["space-3"], -Kit.space["space-3"])
    preview:SetPoint("BOTTOMRIGHT", parent, "BOTTOMRIGHT", -Kit.space["space-3"] - 1, Kit.space["space-3"] + 1)
    preview:SetFrameLevel(parent:GetFrameLevel() + 20)
    preview:EnableMouse(true)
    local heading = Kit.Text(preview, "t-ui-strong", "ink")
    heading:SetPoint("TOPLEFT", Kit.space["space-3"], -Kit.space["space-3"])
    heading:SetText("Sent with every message")
    local close = Kit.Tool(preview, "close", function() Kit.Leave(preview) end)
    close:SetPoint("TOPRIGHT", -Kit.space["space-1"], -Kit.space["space-1"])
    local scroll = Kit.Scroll(preview)
    scroll:SetPoint("TOPLEFT", Kit.space["space-3"], -(Kit.space["space-3"] + Kit.size.control))
    scroll:SetPoint("BOTTOMRIGHT", -Kit.space["space-3"], Kit.space["space-3"])
    local rows = {}
    local id = "|c" .. Kit.Hex("ink-3") .. "%1|r"
    function preview:Open()
        local lines = sheet(T.Context.Text() or "")
        local column = 0
        for index, line in ipairs(lines) do
            local row = rows[index]
            if not row then
                row = CreateFrame("Frame", nil, scroll.child)
                row.rule = Kit.Hairline(row, "line", "TOP")
                row.key = Kit.Text(row, "t-meta", "ink-3")
                row.value = Kit.Text(row, "t-meta", "ink")
                rows[index] = row
            end
            row.key:SetText(line.first and Format.Escape(line.key or "") or "")
            column = math.max(column, row.key:GetUnboundedStringWidth())
        end
        local width, y = scroll:GetWidth(), 0
        scroll.child:SetWidth(width)
        for index, line in ipairs(lines) do
            local row = rows[index]
            local start = line.first and index > 1
            local top = start and Kit.space["space-1"] + Kit.space["space-1"] / 2 or Kit.space["space-1"] / 2
            y = y + (start and Kit.space["space-1"] or 0)
            row.rule:SetShown(start)
            row.value:SetWidth(width - column - Kit.space["space-3"])
            row.value:SetText((Format.Escape(line.value):gsub("(%(?%a+:%d+%)?)", id)))
            Kit.Place(row.key, row, 0, top)
            Kit.Place(row.value, row, column + Kit.space["space-3"], top)
            local height = top + Kit.TextHeight(row.value) + Kit.space["space-1"] / 2
            row:ClearAllPoints()
            row:SetPoint("TOPLEFT", 0, -y)
            row:SetPoint("RIGHT")
            row:SetHeight(height)
            row:Show()
            y = y + height
        end
        for index = #lines + 1, #rows do
            rows[index]:Hide()
        end
        scroll.child:SetHeight(y)
        scroll:ScrollTo(0, true)
        Kit.Rise(self)
        for index = 1, #lines do
            Kit.Rise(rows[index], Kit.Stagger(index))
        end
    end
    preview:Hide()
    return preview
end
