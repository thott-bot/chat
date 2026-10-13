local _, T = ...

local Kit, Format, Store, Actions, Popovers = T.Kit, T.Format, T.Store, T.Actions, T.Popovers
local Window = {}
T.Window = Window

local SIZE, MIN, MAX = { 420, 560 }, { 360, 400 }, { 720, 900 }
local MINI = { 280, 220, 420 }
local BAR = Kit.size["control-lg"]
local PAD = Kit.space["space-4"]
local SIDE = 200
local PROBLEMS = {
    uninstalled = { "Restart WoW to finish setup." },
    exhausted = { "Live updates are paused for this session.", "Reload" },
    low = { "Live updates run out soon this session.", "Reload" },
    unreadable = { "Restart WoW to finish setup." },
    unencodable = { "That message couldn't be sent. Try rewording it." },
    oversized = { "That message is too long to send from the game." },
    unready = { "The thott.bot chat app hasn't shared your projects and models yet. Try again in a moment." },
    DISABLED = { "thott.bot chat's update channels are turned off in the AddOns list. Turn the thott.bot chat group back on.", "Reload" },
}
local AWAY = {
    offline = { "Connect your AI", "Start the thott.bot chat app on this computer. The app connects to the Codex CLI or the Claude Code CLI.", true },
    restart = { "Restart WoW", "The thott.bot chat app updates the addon when WoW closes.", false, true },
}

local frame, bar, side, well, composer, transcript, list, first, notice, glance, rename
local settingsMenu, partsMenu, picker, preview
local selected, screen, folding = nil, "thread", false

local function layout()
    return T.settings.window
end

local function current()
    return screen ~= "new" and Store.Find(selected) or nil
end

local function online()
    return Store.received and not AWAY[Store.codex]
end

local function title()
    if not online() or layout().mini then
        return "thott.bot chat"
    elseif screen == "new" then
        return "New conversation"
    end
    local thread = current()
    return thread and Store.Title(thread) or "thott.bot chat"
end

local function about(thread)
    local label = thread.model
    for _, model in ipairs(Store.models) do
        if model.id == thread.model then
            label = model.label
        end
    end
    local parts = {}
    for _, part in ipairs({ label or "", thread.effort or "", thread.contextLeft and thread.contextLeft .. "% context left" or "" }) do
        if part ~= "" then
            table.insert(parts, Format.Escape(part))
        end
    end
    return table.concat(parts, " · ")
end

local function applyScale()
    frame:SetScale(T.settings.scale)
    Kit.Snap()
end

local function applyOpacity()
    for _, region in ipairs({ bar, composer, well, side, glance }) do
        region.fill:SetAlpha(T.settings.opacity)
    end
end

local function save()
    local point, _, relative, x, y = frame:GetPoint(1)
    local saved = layout()
    saved.point, saved.relative, saved.x, saved.y = point, relative, x, y
    if saved.mini then
        saved.miniWidth = frame:GetWidth()
    else
        saved.width, saved.height = frame:GetWidth(), frame:GetHeight()
    end
end

local function fullWidth()
    local extra = layout().sidebar and SIDE or 0
    return math.max(layout().width or SIZE[1] + extra, MIN[1] + extra)
end

local function dock()
    local open = layout().sidebar
    local extra = open and SIDE or 0
    well:SetPoint("TOPLEFT", open and side or bar, open and "TOPRIGHT" or "BOTTOMLEFT")
    composer:SetPoint("BOTTOMLEFT", open and side or frame, open and "BOTTOMRIGHT" or "BOTTOMLEFT", open and 0 or 1, open and 0 or 1)
    frame:SetResizeBounds(MIN[1] + extra, MIN[2], MAX[1] + extra, MAX[2])
    bar.side:SetHeld(open)
end

local function applyMini()
    local mini = layout().mini
    well:SetShown(not mini)
    side:SetShown(layout().sidebar and not mini)
    composer:SetShown(not mini and online())
    glance:SetShown(mini)
    for _, tool in ipairs({ bar.side, bar.new, bar.gear, bar.divider }) do
        tool:SetShown(not mini)
    end
    bar.heading:ClearAllPoints()
    if mini then
        bar.heading:SetPoint("TOPLEFT", Kit.space["space-3"], 0)
        bar.heading:SetPoint("BOTTOMLEFT", Kit.space["space-3"], 0)
        frame:SetResizeBounds(MINI[2], BAR + 2, MINI[3], MAX[2])
    else
        bar.heading:SetPoint("TOPLEFT", bar.side, "TOPRIGHT", Kit.space["space-2"], 0)
        bar.heading:SetPoint("BOTTOMLEFT", bar.side, "BOTTOMRIGHT", Kit.space["space-2"], 0)
        dock()
    end
    frame.grip:SetAlpha(mini and 0.6 or 1)
    bar.mini.glyph:SetGlyph(mini and "restore" or "minimize")
end

local function size()
    if layout().mini then
        frame:SetSize(layout().miniWidth or MINI[1], BAR + 2)
    else
        frame:SetSize(fullWidth(), layout().height or SIZE[2])
    end
end

local function pin(corner)
    local x, top = corner == "TOPLEFT" and frame:GetLeft() or frame:GetRight(), frame:GetTop()
    frame:ClearAllPoints()
    frame:SetPoint(corner, UIParent, "BOTTOMLEFT", x, top)
end

local function setMini(mini)
    pin("TOPLEFT")
    save()
    layout().mini = mini
    local fromWidth, fromHeight = frame:GetSize()
    local toWidth, toHeight
    if mini then
        toWidth, toHeight = layout().miniWidth or MINI[1], BAR + 2 + glance:Render()
        glance:Hide()
        for _, region in ipairs({ well, side, composer }) do
            Kit.Leave(region)
        end
    else
        toWidth, toHeight = fullWidth(), layout().height or SIZE[2]
        Kit.Leave(glance)
    end
    folding = true
    Kit.Tween(frame, Kit.motion[mini and "motion-2" or "motion-3"], function(progress)
        frame:SetSize(fromWidth + (toWidth - fromWidth) * progress, fromHeight + (toHeight - fromHeight) * progress)
    end, function()
        folding = false
        applyMini()
        size()
        Window.Refresh()
        if mini then
            glance:Reveal()
        else
            for _, region in ipairs({ side, well, composer }) do
                if region:IsShown() then
                    Kit.Rise(region)
                end
            end
        end
    end)
end

local function restore(id)
    setMini(false)
    Window.Select(id)
end

local function startRename()
    local thread = current()
    if layout().mini or not online() or not thread or thread.id:find("^local:") then
        return
    end
    rename.id = thread.id
    rename:SetText(Store.Title(thread))
    rename:Show()
    bar.title:Hide()
    Window.Refresh()
    rename:SetFocus()
    rename:HighlightText()
end

local function finishRename(commit)
    if not rename:IsShown() then
        return
    end
    rename:Hide()
    bar.title:Show()
    rename:ClearFocus()
    local thread = Store.Find(rename.id)
    local text = strtrim(rename:GetText())
    if commit and thread and text ~= "" and text ~= Store.Title(thread) then
        Actions.Rename(thread, text)
    end
    Window.Refresh()
end

function Window.Refresh()
    if not frame or not frame:IsShown() then
        return
    end
    local now = time()
    local thread = current()
    if screen == "thread" and not thread then
        local top = Store.Threads()[1]
        selected = top and top.id
        thread = Store.Find(selected)
        if not thread then
            screen = "new"
        end
    end
    local live = online()
    local mini = layout().mini
    if rename:IsShown() and (mini or rename.id ~= Window.Current()) then
        finishRename(true)
    end
    bar.title:SetFontObject(Kit.Font(mini and "t-label" or "t-title"))
    bar.title:SetText(Format.Escape(title()))
    bar.mark:SetShown(mini)
    bar.spinner:SetShown(not mini and thread ~= nil and Store.Rank(thread) == 1)
    local before = mini and bar.mark or bar.spinner:IsShown() and bar.spinner or nil
    if before then
        bar.title:SetPoint("LEFT", before, "RIGHT", Kit.space["space-2"], 0)
    else
        bar.title:SetPoint("LEFT", bar.heading, "LEFT")
    end
    local waiting = Store.Waiting(thread and thread.id)
    bar.badge:SetShown(not mini and live and waiting > 0 and not side:IsShown())
    bar.badge.text:SetText(waiting)
    bar.badge:SetWidth(math.max(Kit.space["space-4"], bar.badge.text:GetUnboundedStringWidth() + 2 * Kit.space["space-1"]))
    bar.new:SetShown(live and not mini)
    local lead = before and before:GetWidth() + Kit.space["space-2"] or 0
    local badge = bar.badge:IsShown() and bar.badge:GetWidth() + Kit.space["space-2"] or 0
    local room
    if mini then
        room = bar:GetWidth() - 2 * Kit.size.control - Kit.space["space-1"] - Kit.space["space-3"] - Kit.space["space-2"]
    else
        room = bar:GetWidth() - 5 * Kit.size.control - bar.divider:GetWidth() - 2 * Kit.space["space-1"] - 2 * Kit.space["space-2"] - badge
    end
    bar.heading:SetWidth(rename:IsShown() and room or math.min(room, lead + bar.title:GetUnboundedStringWidth()))
    if mini and not folding then
        frame:SetHeight(BAR + 2 + glance:Render())
    end

    if live and Store.codex ~= "ready" and Store.note ~= "" then
        notice:Set(Format.Escape(Store.note), false, Store.codex == "error" and "status" or "setup")
    elseif notice.kind == "setup" or notice.kind == "status" then
        notice:Hide()
    end
    first:SetShown(not live)
    if not live then
        local away = AWAY[Store.codex]
        first.heading:SetText(away[1])
        first.body:SetText(away[2])
        first.retry:SetShown(away[3])
        first.warning:SetShown(away[4])
        first.heading:SetPoint("BOTTOM", first.body, "TOP", away[4] and (first.warning:GetWidth() + Kit.space["space-2"]) / 2 or 0, Kit.space["space-3"])
    end
    transcript:SetShown(live and screen == "thread" and thread ~= nil)
    composer:SetShown(live and not layout().mini and not folding)

    if side:IsShown() then
        list:Render(Window.Current(), now)
    end
    if transcript:IsShown() then
        transcript:Render(thread, now)
        Actions.Read(thread)
    end
    if composer:IsShown() then
        local pick = screen == "new" and Actions.Choice()
        local enabled = false
        for _, part in ipairs(T.Context.parts) do
            enabled = enabled or T.settings.context[part.key]
        end
        composer:Set({
            placeholder = screen == "new" and "Ask anything" or thread.state == "error" and "Reply to try again" or "Reply",
            disabled = thread ~= nil and thread.state == "queued",
            context = T.Context.Summary(),
            contextOn = enabled and T.settings.extras,
            choice = pick and pick.model and Format.Escape(string.format("%s · %s · %s", pick.model.label, pick.effort or "", pick.project and pick.project.label or "")) or nil,
            about = thread and about(thread) or nil,
        })
    end
    if partsMenu:IsShown() then
        partsMenu:Refresh()
    end
end

function Window.SetSidebar(open)
    pin("TOPRIGHT")
    layout().sidebar = open
    dock()
    frame:SetWidth(frame:GetWidth() + (open and SIDE or -SIDE))
    applyMini()
    save()
    Window.Refresh()
    if side:IsShown() then
        list:Reveal()
    end
end

function Window.Select(id)
    selected, screen = id, "thread"
    Window.Refresh()
end

function Window.Show(id)
    if id then
        selected, screen = id, "thread"
    end
    if not frame:IsShown() then
        frame:Show()
        Kit.Rise(frame)
    end
    T.Sync.Fresh()
    Window.Refresh()
end

function Window.Toggle()
    if frame:IsShown() then
        frame:Hide()
    else
        Window.Show()
    end
end

function Window.IsShown()
    return frame ~= nil and frame:IsShown()
end

function Window.Current()
    return screen == "thread" and selected or nil
end

local function buildBar()
    bar = CreateFrame("Frame", nil, frame)
    bar:SetPoint("TOPLEFT", 1, -1)
    bar:SetPoint("TOPRIGHT", -1, -1)
    bar:SetHeight(BAR)
    bar.fill = Kit.Fill(bar, "surface-1")
    Kit.Hairline(bar, "bevel-light", "TOP")
    Kit.Hairline(bar, "edge", "BOTTOM")
    bar:EnableMouse(true)
    bar:RegisterForDrag("LeftButton")
    bar:SetScript("OnDragStart", function() frame:StartMoving() end)
    bar:SetScript("OnDragStop", function()
        frame:StopMovingOrSizing()
        save()
    end)

    bar.close = Kit.Tool(bar, "close", function() frame:Hide() end)
    bar.close:SetPoint("RIGHT", -Kit.space["space-1"], 0)
    bar.mini = Kit.Tool(bar, "minimize", function()
        setMini(not layout().mini)
        Window.Refresh()
    end)
    bar.mini:SetPoint("RIGHT", bar.close, "LEFT")
    bar.divider = CreateFrame("Frame", nil, bar)
    bar.divider:SetSize(2 * Kit.space["space-1"] + 1, Kit.space["space-3"])
    bar.divider:SetPoint("RIGHT", bar.mini, "LEFT")
    Kit.Hairline(bar.divider, "line", "LEFT", { offset = Kit.space["space-1"] })
    bar.gear = Kit.Tool(bar, "gear", function() settingsMenu:Toggle() end)
    bar.gear:SetPoint("RIGHT", bar.divider, "LEFT")
    bar.new = Kit.Tool(bar, "plus", function()
        screen = "new"
        Window.Refresh()
        composer:Focus()
    end)
    bar.new:SetPoint("RIGHT", bar.gear, "LEFT")

    bar.side = Kit.Tool(bar, "sidebar", function() Window.SetSidebar(not layout().sidebar) end)
    bar.side:SetPoint("LEFT", Kit.space["space-1"], 0)
    bar.heading = CreateFrame("Button", nil, bar)
    bar.heading:RegisterForDrag("LeftButton")
    bar.heading:SetScript("OnDragStart", bar:GetScript("OnDragStart"))
    bar.heading:SetScript("OnDragStop", bar:GetScript("OnDragStop"))
    bar.heading:SetScript("OnDoubleClick", startRename)
    bar.spinner = Kit.Spinner(bar.heading)
    bar.spinner:SetPoint("LEFT")
    bar.mark = Kit.Mark(bar.heading, "mark-foot")
    bar.mark:SetPoint("LEFT")
    bar.title = Kit.Text(bar.heading, "t-title", "ink")
    bar.title:SetPoint("RIGHT")
    bar.title:SetWordWrap(false)

    bar.badge = CreateFrame("Frame", nil, bar)
    bar.badge:SetHeight(Kit.space["space-4"])
    bar.badge:SetPoint("LEFT", bar.heading, "RIGHT", Kit.space["space-2"], 0)
    Kit.Fill(bar.badge, "agent-needs")
    Kit.Edge(bar.badge)
    bar.badge.text = Kit.Text(bar.badge, "t-num", "bg")
    bar.badge.text:SetPoint("CENTER")
    Kit.Tip(bar.heading, bar.title)

    rename = CreateFrame("EditBox", nil, bar)
    rename:SetHeight(Kit.size.control)
    rename:SetPoint("LEFT", bar.title, "LEFT", -Kit.space["space-1"], 0)
    rename:SetPoint("RIGHT", bar.heading, "RIGHT", Kit.space["space-1"], 0)
    rename:SetAutoFocus(false)
    rename:SetFontObject(Kit.Font("t-title"))
    rename:SetTextColor(Kit.Color("ink"))
    rename:SetTextInsets(Kit.space["space-1"], Kit.space["space-1"], 0, 0)
    Kit.Fill(rename, "well")
    for _, edge in ipairs(Kit.Edge(rename)) do
        Kit.Paint(edge, "accent")
    end
    rename:SetScript("OnEnterPressed", function() finishRename(true) end)
    rename:SetScript("OnEscapePressed", function() finishRename(false) end)
    rename:SetScript("OnEditFocusLost", function() finishRename(true) end)
    rename:Hide()
end

local function buildFirst()
    first = CreateFrame("Frame", nil, well)
    first:SetAllPoints()
    first.body = Kit.Text(first, "t-body", "ink-2")
    first.body:SetWidth(320)
    first.body:SetJustifyH("CENTER")
    first.body:SetPoint("CENTER", 0, Kit.space["space-2"])
    first.heading = Kit.Text(first, "t-title", "ink")
    first.heading:SetPoint("BOTTOM", first.body, "TOP", 0, Kit.space["space-3"])
    first.warning = Kit.Glyph(first, "warning", "agent-needs")
    first.warning:SetPoint("RIGHT", first.heading, "LEFT", -Kit.space["space-2"], 0)
    first.retry = Kit.Key(first, "Try again")
    first.retry:SetPoint("TOP", first.body, "BOTTOM", 0, -Kit.space["space-3"])
    first.retry:SetScript("OnClick", T.Sync.Now)
end

local function buildNotice()
    notice = CreateFrame("Frame", nil, well)
    notice:SetPoint("TOPLEFT")
    notice:SetPoint("TOPRIGHT")
    notice:SetHeight(Kit.size["control-lg"] + Kit.space["space-2"])
    notice:SetFrameLevel(well:GetFrameLevel() + 10)
    Kit.Fill(notice, "surface-2")
    Kit.Hairline(notice, "line", "BOTTOM")
    notice.stripe = Kit.Stripe(notice, "agent-needs")
    notice.stripe:SetPoint("TOPLEFT")
    notice.stripe:SetPoint("BOTTOMLEFT")
    notice.warning = Kit.Glyph(notice, "warning", "agent-needs")
    notice.warning:SetPoint("LEFT", PAD, 0)
    notice.text = Kit.Text(notice, "t-ui", "ink")
    notice.close = Kit.Tool(notice, "close", function() notice:Hide() end)
    notice.close:SetPoint("RIGHT", -Kit.space["space-1"], 0)
    notice.action = Kit.Key(notice, "Reload")
    notice.action:SetPoint("RIGHT", notice.close, "LEFT", -Kit.space["space-1"], 0)
    notice.action:SetScript("OnClick", C_UI.Reload)
    notice.text:SetPoint("RIGHT", notice.action, "LEFT", -Kit.space["space-2"], 0)
    notice.text:SetWordWrap(false)
    function notice:Set(text, reload, kind)
        local asks = kind ~= "status"
        self.kind = kind
        self.stripe:SetShown(asks)
        self.warning:SetShown(asks)
        if asks then
            self.text:SetPoint("LEFT", self.warning, "RIGHT", Kit.space["space-2"], 0)
        else
            self.text:SetPoint("LEFT", PAD, 0)
        end
        self.text:SetText(text)
        self.action:SetShown(reload)
        self:Show()
    end
    notice:SetScript("OnHide", function(self) self.kind = nil end)
    notice:Hide()
end

local function buildGrip()
    frame.grip = CreateFrame("Button", nil, frame)
    frame.grip:SetSize(Kit.space["space-4"], Kit.space["space-4"])
    frame.grip:SetPoint("BOTTOMRIGHT", -1, 1)
    frame.grip:SetFrameLevel(frame:GetFrameLevel() + 30)
    local glyph = Kit.Glyph(frame.grip, "grip")
    glyph:SetPoint("CENTER")
    frame.grip:SetScript("OnEnter", function() glyph:SetInk("ink") end)
    frame.grip:SetScript("OnLeave", function() glyph:SetInk("ink-3") end)
    frame.grip:SetScript("OnMouseDown", function() frame:StartSizing(layout().mini and "RIGHT" or "BOTTOMRIGHT") end)
    frame.grip:SetScript("OnMouseUp", function()
        frame:StopMovingOrSizing()
        save()
    end)
end

function Window.Init()
    frame = CreateFrame("Frame", "thott_bot_chat_window", UIParent)
    frame:SetFrameStrata("HIGH")
    frame:SetToplevel(true)
    frame:SetClampedToScreen(true)
    frame:SetMovable(true)
    frame:SetResizable(true)
    frame:SetResizeBounds(MIN[1], MIN[2], MAX[1], MAX[2])
    frame:EnableMouse(true)
    Kit.Edge(frame)
    table.insert(UISpecialFrames, "thott_bot_chat_window")

    local saved = layout()
    size()
    if saved.point then
        frame:SetPoint(saved.point, UIParent, saved.relative, saved.x, saved.y)
    else
        frame:SetPoint("CENTER")
    end
    Popovers.Settle()
    applyScale()

    buildBar()
    composer = T.Composer.Build(frame, {
        send = function(text)
            local thread = current()
            if screen == "new" then
                local task = Actions.Ask(text)
                if task then
                    Window.Select(task.id)
                end
                return task ~= nil
            end
            return thread and Actions.Reply(thread, text) ~= nil
        end,
        context = function() partsMenu:Toggle() end,
        choice = function() picker:Toggle() end,
    })
    side = CreateFrame("Frame", nil, frame)
    side:SetPoint("TOPLEFT", bar, "BOTTOMLEFT")
    side:SetPoint("BOTTOMLEFT", 1, 1)
    side:SetWidth(SIDE)
    side.fill = Kit.Fill(side, "well")
    Kit.Hairline(side, "bevel-shade", "TOP")
    Kit.Hairline(side, "edge", "RIGHT")
    list = T.List.Build(side, Window.Select)
    glance = T.Glance.Build(frame, restore)
    glance:SetPoint("TOPLEFT", bar, "BOTTOMLEFT")
    glance:SetPoint("TOPRIGHT", bar, "BOTTOMRIGHT")
    list:SetPoint("TOPLEFT")
    list:SetPoint("BOTTOMRIGHT", -1, 0)

    composer:SetPoint("BOTTOMRIGHT", -1, 1)

    well = CreateFrame("Frame", nil, frame)
    well:SetPoint("TOPRIGHT", bar, "BOTTOMRIGHT")
    well:SetPoint("BOTTOM", composer, "TOP")
    composer:HookScript("OnHide", function() well:SetPoint("BOTTOM", frame, "BOTTOM", 0, 1) end)
    composer:HookScript("OnShow", function() well:SetPoint("BOTTOM", composer, "TOP") end)
    well.fill = Kit.Fill(well, "well")
    Kit.Hairline(well, "bevel-shade", "TOP")
    applyOpacity()

    transcript = T.Transcript.Build(well, {
        refresh = Window.Refresh,
        resend = function(seq) Actions.Resend(current(), seq) end,
        answer = function(allow) Actions.Answer(current(), allow) end,
        stop = function() Actions.Stop(current()) end,
    })
    transcript:SetAllPoints()
    buildFirst()
    buildNotice()
    settingsMenu = Popovers.Settings(bar.gear, { scale = applyScale, opacity = applyOpacity })
    partsMenu = Popovers.Parts(composer.context, { refresh = Window.Refresh, preview = function() preview:Open() end })
    picker = Popovers.Picker(composer.choice)
    preview = Popovers.Preview(frame, well)
    buildGrip()

    frame:SetScript("OnSizeChanged", function()
        if frame:IsShown() and not folding then
            Window.Refresh()
        end
    end)
    frame:HookScript("OnShow", T.Alerts.Refresh)
    frame:HookScript("OnHide", function()
        settingsMenu:Hide()
        partsMenu:Hide()
        picker:Hide()
        T.Alerts.Refresh()
    end)
    frame:Hide()
    dock()
    applyMini()

    T.On("StoreChanged", Window.Refresh)
    local loads = CreateFrame("Frame")
    loads:RegisterEvent("ITEM_DATA_LOAD_RESULT")
    loads:RegisterEvent("SPELL_DATA_LOAD_RESULT")
    loads:SetScript("OnEvent", function(_, event, id, success)
        if Format.Loaded((event == "ITEM_DATA_LOAD_RESULT" and "item:" or "spell:") .. id, success) then
            Window.Refresh()
        end
    end)
    T.On("ThreadReplaced", function(old, new)
        if selected == old then
            selected = new
        end
    end)
    T.On("Problem", function(reason)
        local problem = PROBLEMS[reason] or { "Live updates stopped: " .. tostring(reason) .. ".", "Reload" }
        notice:Set(problem[1], problem[2] ~= nil, "problem")
    end)
    C_Timer.NewTicker(1, function()
        if not frame:IsShown() then
            return
        end
        local now = time()
        if transcript:IsShown() then
            transcript:Tick(now)
        end
        if side:IsShown() then
            list:Render(Window.Current(), now)
        end
    end)
end
