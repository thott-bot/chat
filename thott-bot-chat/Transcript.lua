local _, T = ...

local Kit, Format, Store = T.Kit, T.Format, T.Store
local Transcript = {}
T.Transcript = Transcript

local PAD = Kit.space["space-4"]
local TURN = Kit.space["space-6"]
local GAP = Kit.space["space-2"]

local function linked(block)
    block:SetHyperlinksEnabled(true)
    block:SetScript("OnHyperlinkEnter", function(self, link)
        GameTooltip:SetOwner(self, "ANCHOR_CURSOR")
        GameTooltip:SetHyperlink(link)
        GameTooltip:Show()
    end)
    block:SetScript("OnHyperlinkLeave", function() GameTooltip:Hide() end)
    block:SetScript("OnHyperlinkClick", function(self, link, text, button) SetItemRef(link, text, button, self) end)
    return block
end

local make = {}

function make.user(child)
    local block = CreateFrame("Frame", nil, child)
    block.bubble = Kit.Panel(CreateFrame("Frame", nil, block), "surface-3")
    block.bubble:SetPoint("TOPRIGHT")
    block.text = Kit.Text(block.bubble, "t-body", "ink")
    block.failed = Kit.Text(block, "t-meta", "agent-error")
    block.failed:SetText("Not delivered ·")
    block.resend = Kit.Link(block, "t-ui", "agent-error", "ink")
    block.resend:SetLabel("Resend")
    block.resend:SetPoint("LEFT", block.failed, "RIGHT", Kit.space["space-1"], 0)
    block.resend:SetScript("OnClick", function() block.onResend() end)
    return block
end

function make.steps(child)
    local block = CreateFrame("Frame", nil, child)
    block.summary = CreateFrame("Button", nil, block)
    block.summary:SetPoint("TOPLEFT")
    block.summary:SetHeight(Kit.space["space-4"])
    block.tick = Kit.Glyph(block.summary, "check", "accent")
    block.tick:SetPoint("LEFT")
    block.text = Kit.Text(block.summary, "t-ui", "ink-3")
    block.text:SetPoint("LEFT", block.tick, "RIGHT", GAP, 0)
    block.chevron = Kit.Glyph(block.summary, "down")
    block.chevron:SetPoint("LEFT", block.text, "RIGHT", GAP, 0)
    block.summary:SetScript("OnEnter", function() Kit.Paint(block.text, "ink-2") block.chevron:Show() end)
    block.summary:SetScript("OnLeave", function() Kit.Paint(block.text, "ink-3") block.chevron:SetShown(block.open) end)
    block.summary:SetScript("OnClick", function() block.onToggle() end)
    block.items = {}
    return block
end

function make.paragraph(child)
    local block = linked(CreateFrame("Frame", nil, child))
    block.text = Kit.Text(block, "t-body", "ink")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.item(child)
    local block = linked(CreateFrame("Frame", nil, child))
    block.mark = block:CreateTexture(nil, "ARTWORK")
    block.mark:SetSize(Kit.space["space-1"], Kit.space["space-1"])
    block.mark:SetPoint("TOPLEFT", Kit.space["space-1"], -GAP)
    Kit.Paint(block.mark, "ink-3")
    block.text = Kit.Text(block, "t-body", "ink")
    Kit.Place(block.text, block, Kit.space["space-4"], 0)
    return block
end

function make.note(child)
    local block = CreateFrame("Frame", nil, child)
    block.text = Kit.Text(block, "t-meta", "agent-error")
    Kit.Place(block.text, block, 0, 0)
    return block
end

function make.card(child)
    local block = Kit.Panel(CreateFrame("Frame", nil, child), "surface-1")
    block.stripe = Kit.Stripe(block, "agent-needs")
    block.title = Kit.Text(block, "t-ui-strong", "ink")
    Kit.Place(block.title, block, PAD, Kit.space["space-3"])
    block.command = Kit.Recess(CreateFrame("Frame", nil, block))
    block.commandText = Kit.Text(block.command, "t-ui", "ink")
    block.reason = Kit.Text(block, "t-meta", "ink-2")
    block.allow = Kit.Key(block, "Allow", true)
    block.allow:SetPoint("BOTTOMRIGHT", -Kit.space["space-3"], Kit.space["space-3"])
    block.deny = Kit.Key(block, "Deny")
    block.deny:SetPoint("RIGHT", block.allow, "LEFT", -GAP, 0)
    block.allow:SetScript("OnClick", function() block.onAnswer(true) end)
    block.deny:SetScript("OnClick", function() block.onAnswer(false) end)
    return block
end

function make.working(child)
    local block = CreateFrame("Frame", nil, child)
    block.spinner = Kit.Spinner(block)
    block.spinner:SetPoint("LEFT")
    block.text = Kit.Text(block, "t-ui", "ink-3")
    block.text:SetPoint("LEFT", block.spinner, "RIGHT", GAP, 0)
    block.stop = Kit.Link(block, "t-ui", "ink-3", "ink")
    block.stop:SetLabel("Stop")
    block.stop:SetPoint("RIGHT")
    block.stop:SetScript("OnClick", function() block.onStop() end)
    block.clock = Kit.Text(block, "t-meta", "ink-3")
    block.clock:SetPoint("RIGHT", block.stop, "LEFT", -GAP, 0)
    block.text:SetPoint("RIGHT", block.clock, "LEFT", -GAP, 0)
    block.text:SetWordWrap(false)
    return block
end

function Transcript.Build(parent, handlers)
    local scroll = Kit.Scroll(parent)
    local child = scroll.child
    local pools, open, opening = {}, {}, {}
    local shown, anchor, tail, clock

    local function stamp(turn)
        return turn.at and (turn.role .. "@" .. turn.at)
    end

    local function take(kind)
        local pool = pools[kind] or { used = 0 }
        pools[kind] = pool
        pool.used = pool.used + 1
        pool[pool.used] = pool[pool.used] or make[kind](child)
        local block = pool[pool.used]
        block:Show()
        return block
    end

    local function place(block, y, height)
        block:ClearAllPoints()
        block:SetPoint("TOPLEFT", child, "TOPLEFT", PAD, -y)
        block:SetPoint("RIGHT", child, "RIGHT", -PAD, 0)
        block:SetHeight(height)
        return y + height
    end

    local function measure(text, width)
        text:SetWidth(width)
        return Kit.TextHeight(text)
    end

    local function user(turn, y, width)
        local block = take("user")
        local limit = math.floor(width * 0.85)
        block.text:SetText(Format.Escape(turn.text))
        local bubble = math.min(limit, math.ceil(block.text:GetUnboundedStringWidth()) + 2 * Kit.space["space-3"] + 2)
        local height = measure(block.text, bubble - 2 * Kit.space["space-3"] - 2) + 2 * GAP + 2
        block.text:ClearAllPoints()
        Kit.Place(block.text, block.bubble, Kit.space["space-3"] + 1, GAP + 1)
        block.bubble:SetSize(bubble, height)
        local failed = turn.seq and Store.Delivery(turn.seq) == "failed"
        block.failed:SetShown(failed)
        block.resend:SetShown(failed)
        block.onResend = function() handlers.resend(turn.seq) end
        if failed then
            block.failed:ClearAllPoints()
            block.failed:SetPoint("TOPRIGHT", block.bubble, "BOTTOMRIGHT", -block.resend:GetWidth() - Kit.space["space-1"], -Kit.space["space-1"])
            height = height + Kit.space["space-1"] + Kit.space["space-4"]
        end
        return block, place(block, y, height)
    end

    local function steps(turn, key, y)
        local block = take("steps")
        block.open = open[key]
        block.text:SetText(Format.Escape(turn.text))
        block.summary:SetWidth(2 * block.tick:GetWidth() + 2 * GAP + block.text:GetUnboundedStringWidth())
        block.chevron:SetGlyph(block.open and "up" or "down")
        block.chevron:SetShown(block.open)
        block.onToggle = function()
            open[key] = not open[key]
            opening[key] = open[key]
            handlers.refresh()
        end
        local height = Kit.space["space-4"]
        for _, item in ipairs(block.items) do
            item:Hide()
        end
        if block.open then
            for index, line in ipairs(turn.items or {}) do
                local item = block.items[index]
                if not item then
                    item = CreateFrame("Frame", nil, block)
                    item:SetHeight(Kit.space["space-4"])
                    item.tick = Kit.Glyph(item, "check", "line-strong")
                    item.tick:SetPoint("LEFT")
                    item.text = Kit.Text(item, "t-meta", "ink-3")
                    item.text:SetWordWrap(false)
                    item.sep = Kit.Text(item, "t-meta", "ink-3")
                    item.sep:SetPoint("LEFT", item.text, "RIGHT", Kit.space["space-1"], 0)
                    item.sep:SetText("·")
                    item.tool = Kit.Text(item, "t-meta", "ink-2")
                    item.tool:SetPoint("LEFT", item.sep, "RIGHT", Kit.space["space-1"], 0)
                    item.tool:SetPoint("RIGHT")
                    item.tool:SetWordWrap(false)
                    block.items[index] = item
                end
                local server, tool = line:match("^(.-) · (.+)$")
                item.text:ClearAllPoints()
                item.text:SetPoint("LEFT", item.tick, "RIGHT", GAP, 0)
                if not tool then
                    item.text:SetPoint("RIGHT")
                end
                item.text:SetText(Format.Escape(server or line))
                item.sep:SetShown(tool ~= nil)
                item.tool:SetShown(tool ~= nil)
                item.tool:SetText(Format.Escape(tool))
                item:ClearAllPoints()
                item:SetPoint("TOPLEFT", Kit.space["space-6"] - Kit.space["space-1"], -(height + Kit.space["space-1"]))
                item:SetPoint("RIGHT")
                item:Show()
                if opening[key] then
                    Kit.Rise(item, Kit.Stagger(index))
                end
                height = height + Kit.space["space-1"] + Kit.space["space-4"]
            end
        end
        opening[key] = nil
        return block, place(block, y, height)
    end

    local function answer(turn, y, width)
        local first, last = nil, y
        for index, piece in ipairs(Format.Blocks(turn.text, Kit.Hex("ink-strong"))) do
            local block = take(piece.kind == "item" and "item" or "paragraph")
            block.text:SetText(piece.text)
            local indent = piece.kind == "item" and Kit.space["space-4"] or 0
            last = place(block, last + (index > 1 and GAP or 0), measure(block.text, width - indent))
            first = first or block
        end
        return first, last
    end

    local function card(thread, y, width)
        local block = take("card")
        local approval = thread.approval
        Kit.Paint(block.stripe, approval and "agent-needs" or "agent-error")
        local agent = Format.Agent(thread)
        block.title:SetText(agent .. (approval and (approval.kind == "file" and " wants to change files" or " wants to run a command") or " stopped"))
        local inner = width - PAD - Kit.space["space-3"]
        local height = Kit.space["space-3"] + Kit.TextHeight(block.title)
        block.command:SetShown(approval ~= nil)
        if approval then
            block.commandText:SetText(Format.Escape(approval.text))
            block.commandText:ClearAllPoints()
            Kit.Place(block.commandText, block.command, GAP + 1, Kit.space["space-1"] + 1)
            local command = measure(block.commandText, inner - 2 * GAP - 2) + 2 * Kit.space["space-1"] + 2
            block.command:ClearAllPoints()
            block.command:SetPoint("TOPLEFT", PAD, -(height + GAP))
            block.command:SetPoint("TOPRIGHT", -Kit.space["space-3"], -(height + GAP))
            block.command:SetHeight(command)
            height = height + GAP + command
        end
        local reason = approval and approval.reason or thread.error
        if not reason or reason == "" then
            reason = not approval and "Something went wrong. Reply to try again." or nil
        end
        block.reason:SetShown(reason ~= nil)
        if reason then
            block.reason:SetText(Format.Escape(reason))
            block.reason:ClearAllPoints()
            Kit.Place(block.reason, block, PAD, height + GAP)
            height = height + GAP + measure(block.reason, inner)
        end
        local answered = approval and Store.Answered(thread)
        block.allow:SetShown(approval ~= nil)
        block.deny:SetShown(approval ~= nil)
        block.allow:SetEnabled(not answered)
        block.deny:SetEnabled(not answered)
        block.onAnswer = function(allow) handlers.answer(allow) end
        if approval then
            height = height + Kit.space["space-3"] + Kit.size.control
        end
        return block, place(block, y, height + Kit.space["space-3"])
    end

    local function working(thread, y, now, sending)
        local block = take("working")
        local active = thread.state == "active"
        local activity = thread.activity and thread.activity ~= "" and thread.activity or "Working"
        block.text:SetText(sending and "Sending to " .. Format.Agent(thread) or active and activity or "Starting")
        block.clock:SetShown(active)
        block.stop:SetShown(active)
        block.clock:SetText(Format.Clock(now - (thread.since or thread.updated)))
        block.onStop = function() handlers.stop() end
        clock = active and { text = block.clock, since = thread.since or thread.updated } or nil
        return block, place(block, y, Kit.space["space-4"])
    end

    function scroll:Render(thread, now)
        for _, pool in pairs(pools) do
            for index = 1, pool.used do
                pool[index]:Hide()
            end
            pool.used = 0
        end
        clock = nil
        local width = child:GetWidth() - 2 * PAD
        local turns = Store.Turns(thread)
        local fresh
        if shown == thread.id and anchor then
            local kept = 0
            for index, turn in ipairs(turns) do
                if stamp(turn) == anchor then
                    kept = index
                end
            end
            if kept > 0 and #turns > kept + tail then
                fresh = kept + tail + 1
            end
        end
        local atEnd = shown ~= thread.id or self.atEnd
        local y, previous, sending, exchange = PAD, nil, false, 0
        for index, turn in ipairs(turns) do
            local gap = previous and ((previous == "activity" and turn.role == "assistant") and Kit.space["space-3"] or TURN) or 0
            local block
            if turn.role == "user" then
                exchange = turn.at or index
                block, y = user(turn, y + gap, width)
                sending = sending or (turn.seq ~= nil and Store.Delivery(turn.seq) == "sending")
            elseif turn.role == "activity" then
                block, y = steps(turn, thread.id .. "@" .. exchange, y + gap)
            elseif turn.role == "assistant" then
                block, y = answer(turn, y + gap, width)
            else
                block = take("note")
                block.text:SetText(Format.Escape(turn.text))
                y = place(block, y + gap, measure(block.text, width))
            end
            if fresh and index >= fresh and block then
                Kit.Rise(block)
            end
            previous = turn.role
        end
        if thread.approval or thread.state == "error" then
            local _, bottom = card(thread, y + TURN, width)
            y = bottom
        elseif Store.Rank(thread) == 1 or Store.Pending(thread) then
            local _, bottom = working(thread, y + TURN, now, sending)
            y = bottom
        end
        child:SetHeight(y + PAD)
        shown, anchor, tail = thread.id, nil, 0
        for index, turn in ipairs(turns) do
            if stamp(turn) then
                anchor, tail = stamp(turn), #turns - index
            end
        end
        if atEnd then
            self:ScrollToEnd()
        end
    end

    function scroll:Tick(now)
        if clock then
            clock.text:SetText(Format.Clock(now - clock.since))
        end
    end

    return scroll
end
