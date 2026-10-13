local _, T = ...

local Kit, Format, Store = T.Kit, T.Format, T.Store
local List = {}
T.List = List

local ROW = 52
local HEAD = Kit.size["control-lg"]
local PAD = Kit.space["space-4"]

local function row(child, onSelect)
    local button = CreateFrame("Button", nil, child)
    button:SetHeight(ROW)
    button.hover = Kit.Fill(button, "surface-1")
    button.hover:Hide()
    button.current = Kit.Fill(button, "surface-2", "BACKGROUND", 1)
    button.rule = Kit.Hairline(button, "line", "TOP", { indent = PAD })
    button.mark = button:CreateTexture(nil, "ARTWORK")
    button.mark:SetSize(Kit.space["space-2"], Kit.space["space-2"])
    button.mark:SetPoint("TOPLEFT", Kit.space["space-3"], -(ROW / 2 - Kit.space["space-4"] + Kit.space["space-1"]))
    button.spinner = Kit.Spinner(button)
    button.spinner:SetPoint("LEFT", button.mark, "LEFT", 0, 0)
    button.title = Kit.Text(button, "t-ui", "ink")
    button.title:SetPoint("TOPLEFT", Kit.space["space-8"], -(ROW / 2 - Kit.space["space-4"]))
    button.title:SetPoint("RIGHT", -PAD, 0)
    button.title:SetHeight(Kit.space["space-4"])
    button.title:SetJustifyV("MIDDLE")
    button.title:SetWordWrap(false)
    button.status = Kit.Text(button, "t-meta", "ink-3")
    button.status:SetPoint("TOPLEFT", button.title, "BOTTOMLEFT")
    button.status:SetPoint("RIGHT", -PAD, 0)
    button.status:SetHeight(Kit.space["space-4"])
    button.status:SetJustifyV("MIDDLE")
    button.status:SetWordWrap(false)
    button:SetScript("OnEnter", function(self) self.hover:SetShown(not self.isCurrent) end)
    button:SetScript("OnLeave", function(self) self.hover:Hide() end)
    button:SetScript("OnClick", function(self) onSelect(self.id) end)
    Kit.Tip(button, button.title)
    return button
end

local function heading(child)
    local text = Kit.Text(child, "t-ui-strong", "ink-2")
    text:SetHeight(Kit.space["space-4"])
    text:SetJustifyV("MIDDLE")
    text:SetWordWrap(false)
    return text
end

local function sorted(order)
    local threads = Store.Threads()
    if order == "recent" then
        table.sort(threads, function(a, b) return (a.updated or 0) > (b.updated or 0) end)
    end
    return threads
end

local function grouped(threads, byProject)
    if not byProject then
        return { { project = "", list = threads } }
    end
    local groups, seen = {}, {}
    for _, thread in ipairs(threads) do
        local project = thread.project or ""
        if not seen[project] then
            seen[project] = { project = project, list = {} }
            table.insert(groups, seen[project])
        end
        table.insert(seen[project].list, thread)
    end
    return groups
end

function List.Build(parent, onSelect)
    local list = CreateFrame("Frame", nil, parent)
    local head = CreateFrame("Frame", nil, list)
    head:SetPoint("TOPLEFT")
    head:SetPoint("TOPRIGHT")
    head:SetHeight(HEAD)
    Kit.Hairline(head, "line", "BOTTOM")
    local scroll = Kit.Scroll(list)
    scroll:SetPoint("TOPLEFT", head, "BOTTOMLEFT")
    scroll:SetPoint("BOTTOMRIGHT")
    local rows, headings, shown = {}, {}, {}

    local sort = Kit.Link(head, "t-meta", "ink-3", "ink-2", "down")
    sort:SetPoint("LEFT", Kit.space["space-3"], 0)
    local menu = Kit.Popover(sort)
    menu:SetPoint("TOPLEFT", head, "BOTTOMLEFT", Kit.space["space-2"], -Kit.space["space-1"])
    sort:SetScript("OnClick", function()
        if menu:IsShown() then
            Kit.Leave(menu)
        else
            Kit.Rise(menu)
        end
    end)
    menu:HookScript("OnShow", function() sort:SetHeld(true) end)
    menu:HookScript("OnHide", function() sort:SetHeld(false) end)

    local options, y = {}, Kit.space["space-2"]
    local function label(text)
        Kit.Label(menu, text):SetPoint("TOPLEFT", Kit.space["space-3"], -y)
        y = y + Kit.size.control
    end
    local function option(key, value, text)
        local button = Kit.Option(menu, function()
            T.settings.list[key] = value
            list:Render(list.current, time())
        end)
        button:SetPoint("TOPLEFT", Kit.space["space-3"], -y)
        button:SetPoint("RIGHT", -Kit.space["space-3"], 0)
        button.key, button.value, button.name = key, value, text
        table.insert(options, button)
        y = y + Kit.size.control
    end
    label("Group")
    option("group", "project", "By project")
    option("group", "none", "None")
    y = y + Kit.space["space-2"]
    label("Sort")
    option("sort", "recent", "Recent")
    option("sort", "needs", "Needs you first")
    menu:SetSize(168, y + Kit.space["space-3"])

    function list:Render(current, now)
        self.current = current
        local choice = T.settings.list
        local byProject = choice.group == "project"
        sort:SetLabel((byProject and "Projects" or "All") .. " · " .. (choice.sort == "recent" and "Recent" or "Needs you first"))
        for _, button in ipairs(options) do
            button:Set(button.name, choice[button.key] == button.value)
        end

        local top, used, labels, previous = 0, 0, 0, nil
        wipe(shown)
        for _, group in ipairs(grouped(sorted(choice.sort), byProject)) do
            if group.project ~= "" then
                labels = labels + 1
                local text = headings[labels] or heading(scroll.child)
                headings[labels] = text
                text:SetText(Format.Escape(group.project))
                text:ClearAllPoints()
                text:SetPoint("TOPLEFT", Kit.space["space-8"], -(top + HEAD - Kit.space["space-1"] - Kit.space["space-4"]))
                text:SetPoint("RIGHT", -PAD, 0)
                text:Show()
                table.insert(shown, text)
                top = top + HEAD
                previous = nil
            end
            for _, thread in ipairs(group.list) do
                used = used + 1
                local button = rows[used] or row(scroll.child, onSelect)
                rows[used] = button
                button.id = thread.id
                button.isCurrent = thread.id == current
                button.current:SetShown(button.isCurrent)
                button.rule:SetShown(previous ~= nil and not button.isCurrent and not previous.isCurrent)
                button.title:SetFontObject(Kit.Font(Store.IsUnread(thread) and "t-ui-strong" or "t-ui"))
                button.title:SetText(Format.Escape(Store.Title(thread)))
                local status = Format.Agent(thread) .. " · " .. Format.Status(thread, now)
                if not byProject and thread.project then
                    status = status .. " · " .. thread.project
                end
                button.status:SetText(Format.Escape(status))
                local rank = Store.Rank(thread)
                button.mark:SetShown(rank == 0)
                Kit.Paint(button.mark, thread.state == "error" and "agent-error" or "agent-needs")
                button.spinner:SetShown(rank == 1)
                button:ClearAllPoints()
                button:SetPoint("TOPLEFT", scroll.child, "TOPLEFT", 0, -top)
                button:SetPoint("RIGHT", scroll.child, "RIGHT")
                button:Show()
                table.insert(shown, button)
                top = top + ROW
                previous = button
            end
        end
        for index = used + 1, #rows do
            rows[index]:Hide()
        end
        for index = labels + 1, #headings do
            headings[index]:Hide()
        end
        scroll.child:SetHeight(top + Kit.space["space-2"])
    end

    function list:Reveal()
        for index, region in ipairs(shown) do
            Kit.Rise(region, Kit.Stagger(index))
        end
    end

    return list
end
