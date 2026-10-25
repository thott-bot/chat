local here = arg[0]:match("^(.*)[/\\][^/\\]+$") or "."
local wow = dofile(here .. "/wow.lua")
local samples = wow.Samples(here .. "/../samples/samples.json")

local passed = 0
local function check(name, condition)
    if not condition then
        error("failed: " .. name, 2)
    end
    passed = passed + 1
end

local function find(test)
    for _, frame in ipairs(wow.frames) do
        if test(frame) then
            return frame
        end
    end
end

local function tool(name)
    return find(function(frame) return frame.glyph and frame.glyph.name == name and frame._scripts.OnClick end)
end

wow.payload = samples.payloads.offline
local T = wow.Load(here .. "/../thott-bot-chat")

-- Run 0.04 s ticks (FRAME_SECONDS in Transport.lua) until the transport sent all frames of the last packet.
local function settle()
    local last = wow.sent[#wow.sent].seq
    while T.Transport.Sending(last) do
        wow.Run(0.04)
    end
end
check("login loads the first slot", #wow.loaded == 1)
check("login says hello", wow.sent[1].type == "hello")
check("the helper is offline", T.Store.codex == "offline")
check("offline is not something to wait on", not T.Store.Awaiting())

local starting = CopyTable(samples.payloads.ready)
starting.rev, starting.starting, starting.threads = starting.rev - 2, true, {}
wow.payload = starting
T.Sync.Now()
check("an app still starting is waited on", T.Store.Awaiting())
local started = CopyTable(starting)
started.rev, started.starting = started.rev + 1, nil
wow.payload = started
local startLoads = #wow.loaded
wow.Run(2)
check("the next check, 2 s later, reads the started app, and with nothing awaited the checks stop", #wow.loaded == startLoads + 1 and not T.Store.Awaiting())

T.Window.Show()
check("the window opens", T.Window.IsShown())

local function fold(placed)
    local window = thott_bot_chat_window
    local top, left, height = window:GetTop(), window:GetLeft(), window:GetHeight()
    tool("minimize"):Fire("OnClick")
    check("minimize keeps the window", T.Window.IsShown())
    check("the minimized bar sits where the title bar was, wherever " .. placed, window:GetTop() == top and window:GetLeft() == left and window:GetHeight() < height)
    tool("restore"):Fire("OnClick")
    check("restore brings the window back to the same place, wherever " .. placed, window:GetTop() == top and window:GetLeft() == left and window:GetHeight() == height)
end
fold("it first opened")

wow.payload = samples.payloads.ready
T.Sync.Now()
check("the helper's threads arrive", #T.Store.Threads() == 6)
local first = T.Store.Threads()[1]
check("threads needing you come first", first.state == "needs_you" or first.state == "error")
check("two threads need you", T.Store.Waiting() == 2)

for _, thread in ipairs(T.Store.Threads()) do
    T.Window.Select(thread.id)
    check("renders " .. thread.title, T.Window.Current() == thread.id)
end

local mana = T.Store.Find("019a7c2e-0002")
T.Window.Select(mana.id)
local seq = T.Actions.Reply(mana, "Thanks. Where do I train it?")
check("a reply is queued", seq ~= nil)
check("a reply is sending", T.Store.Delivery(seq) == "sending")
check("a sent reply is awaited", T.Store.Awaiting())
check("nothing about the character is sent by default", wow.sent[#wow.sent].contextText == nil)
check("the reply shows in the transcript", T.Store.Turns(mana)[#T.Store.Turns(mana)].pending)
settle()
check("the reply's frames go out", T.Store.Delivery(seq) == "sent")

wow.Run(2)
check("no ack after the window means not delivered", T.Store.Delivery(seq) == "failed")
local resent = T.Actions.Resend(mana, seq)
check("resend queues it again", resent ~= nil and resent ~= seq)
settle()

local acked = CopyTable(samples.payloads.ready)
acked.rev = acked.rev + 1
acked.acks = { [T.Transport.session] = { resent } }
wow.payload = acked
T.Sync.Now()
check("the helper's ack delivers it", T.Store.Delivery(resent) == "delivered")

local approval = T.Store.Find("019a7c2e-0004")
T.Window.Select(approval.id)
T.Actions.Answer(approval, true)
check("allow sends an approve packet", wow.sent[#wow.sent].type == "approve" and wow.sent[#wow.sent].approval == "1-12")
check("the card knows it was answered", T.Store.Answered(approval))

T.Actions.Stop(T.Store.Find("019a7c2e-0003"))
check("stop sends a stop packet", wow.sent[#wow.sent].type == "stop")

tool("plus"):Fire("OnClick")
check("new conversation has no thread", T.Window.Current() == nil)
local task = T.Actions.Ask("Plan my route from Refuge Pointe.")
check("asking starts a local task", task and task.state == "queued")
check("asking sends a create packet", wow.sent[#wow.sent].type == "create" and wow.sent[#wow.sent].model == "gpt-6.1-sol")

local adopted = CopyTable(acked)
adopted.rev = adopted.rev + 1
table.insert(adopted.threads, 1, {
    id = "019a7c2e-0006", title = "Plan my route from Refuge Pointe.", state = "active", project = "wow", cwd = "~/wow",
    model = "gpt-5.5", effort = "medium", updated = wow.now, since = wow.now, unread = false,
    origin = task.origin, turns = { { role = "user", text = "Plan my route from Refuge Pointe.", at = wow.now, context = true } },
})
wow.payload = adopted
T.Sync.Now()
check("the helper's thread replaces the local task", T.Store.Find(task.id) == nil and T.Store.Find("019a7c2e-0006") ~= nil)

local replied = CopyTable(adopted)
replied.rev = replied.rev + 1
replied.acks = { [T.Transport.session] = {} }
for _, packet in ipairs(wow.sent) do
    table.insert(replied.acks[T.Transport.session], packet.seq)
end
replied.threads[1].state = "idle"
for _, thread in ipairs(replied.threads) do
    if thread.state == "active" then
        thread.state = "idle"
    end
end
table.insert(replied.threads[1].turns, { role = "assistant", text = "Head north to **Go'Shek Farm** first.", at = wow.now })
for _, thread in ipairs(replied.threads) do
    if thread.agent == "claude" then
        thread.updated = wow.now
        table.insert(thread.turns, { role = "assistant", text = "Moved it.", at = wow.now })
    end
end
T.Window.Select("019a7c2e-0002")
local chats = #wow.chat
wow.payload = replied
T.Sync.Now()
local said = table.concat(wow.chat, "\n", chats + 1)
check("a finished reply alerts in chat, naming its agent", #wow.chat == chats + 2 and said:find("Codex replied", 1, true) and said:find("Claude replied", 1, true))
check("nothing in flight stops the checks", not T.Store.Awaiting())

local function listed(title)
    return find(function(frame) return frame.status and frame.title and frame.title._text == title and frame:IsVisible() end) ~= nil
end
check("the sidebar lists every conversation", T.settings.window.sidebar and listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnClick")
check("the sidebar closes", not T.settings.window.sidebar and not listed("Running out of mana between pulls"))
tool("sidebar"):Fire("OnClick")

local function row(title)
    return find(function(frame) return frame.status and frame.title and frame.title._text == title and frame:IsVisible() end)
end
local function text(value)
    for _, region in ipairs(wow.texts) do
        if region._text == value and region:IsVisible() then
            return region
        end
    end
end
check("each row starts with its agent", row("Running out of mana between pulls").status._text == "Codex · 4m ago")
check("Claude's rows name Claude", row("Move my mana bar after combat ends").status._text:find("^Claude · ") ~= nil)
check("rows group under their project", text("wow") ~= nil and text("ManaBar") ~= nil)
local sorter = find(function(frame) return frame.text and frame.text._text == "Projects · Recent" and frame._scripts.OnClick end)
check("the sidebar names its grouping and order", sorter ~= nil)
sorter:Fire("OnClick")
local function option(label)
    return find(function(frame) return frame.mark and frame.text and frame.text._text == label and frame._scripts.OnClick end)
end
option("None"):Fire("OnClick")
check("ungrouped rows name their project", T.settings.list.group == "none" and text("ManaBar") == nil and row("Running out of mana between pulls").status._text == "Codex · 4m ago · wow")
option("Needs you first"):Fire("OnClick")
check("the choice of grouping and order is saved", T.settings.list.sort == "needs" and sorter.text._text == "All · Needs you first")
option("By project"):Fire("OnClick")
option("Recent"):Fire("OnClick")

T.Window.Select("019a7c2e-0002")
find(function(frame) return frame.summary and frame:IsVisible() end).summary:Fire("OnClick")
local step = find(function(frame) return frame.tool and frame.tool._text == "fetch" and frame:IsVisible() end)
check("a tool line splits the connection from the tool", step and step.text._text == "thott.bot" and step.sep:IsShown())

tool("gear"):Fire("OnClick")
local function listening()
    return find(function(frame) return frame._events and frame._events.GLOBAL_MOUSE_DOWN end)
end
local popover = listening()
wow.Event("GLOBAL_MOUSE_DOWN", "LeftButton")
check("a click outside an open popover closes it, and a closed popover stops listening for clicks", popover and not popover:IsShown() and listening() == nil)
fold("the sidebar moved it")

local function key(label)
    return find(function(frame) return frame.label and frame.label._text == label end)
end
T.Window.Select("019a7c2e-0002")
local context = find(function(frame) return frame.mark and frame.text and frame._scripts.OnClick and frame.text._text == "Not sending your character" end)
check("the composer says nothing is sent yet", context ~= nil)
local composer = find(function(frame) return frame.about end)
check("an open conversation shows its model and context left", composer.about:IsShown() and composer.about._text == "GPT-6.1-Sol · medium · 84% context left")
local none = true
for _, on in pairs(T.settings.context) do
    none = none and not on
end
check("nothing is sent by default", none)
context:Fire("OnClick")
local all = find(function(frame) return frame.text and frame.text._text == "All" and frame._scripts.OnClick end)
check("All is offered while nothing is sent", all ~= nil)
option("Character"):Fire("OnClick")
option("Location"):Fire("OnClick")
check("None is offered while anything is sent", T.settings.context.character and all.text._text == "None")
all:Fire("OnClick")
check("None clears a partial choice in one click", not T.settings.context.character and not T.settings.context.location and all.text._text == "All")
all:Fire("OnClick")
check("All sends every part", T.settings.context.quests and T.settings.context.reputation and T.settings.context.buffs and all.text._text == "None")
check("gear, bags, reputation and buffs are offered apart", option("Gear") and option("Bags") and option("Reputation") and option("Buffs") and not option("Gear and bags") and not option("Reputation and buffs"))
key("Preview"):Fire("OnClick")
check("preview lays each part out beside its name", text("Quests (1)") ~= nil and text("Foul Magics |c" .. T.Kit.Hex("ink-3") .. "(quest:676)|r level 36 · 2/4 Syndicate Conjuror") ~= nil)
check("the snapshot names what the AI searches by name and writes ids as thott.bot does", T.Context.Text() == table.concat({
    "Game: WoW Forever 1.60.1.70170",
    "Character: level 33 Human Mage, Alliance · XP 41200 / 66300, rested 12000 · 34g 56s 78c",
    "Location: Refuge Pointe, Arathi Highlands 45.0, 47.0 · resting",
    "Quests (1):",
    "  Foul Magics (quest:676) level 36 · 2/4 Syndicate Conjuror",
    "Equipped: item:15275",
    "Bags: item:8079 ×25",
    "Talents: spell 11069 (Rank 2)",
    "Skills: First Aid 150/225",
    "Spells known: Fireball (Rank 1)",
    "Target: Elandra (npc:2601) level 33, hostile",
    "Reputation: Stormwind (faction:72) 3000",
    "Buffs: Arcane Intellect",
}, "\n"))
local off = find(function(frame) return frame.label and frame.label._text == "Off" and frame._parent and frame._parent._parent == all._parent end)
context:Fire("OnClick")
off:Fire("OnClick")
check("one switch turns off everything sent besides the message", not T.settings.extras and T.Context.Text() == nil and T.Context.Summary() == "Sending only your message" and not option("Character"):IsEnabled())
T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Just this")
check("a message sent with it off asks the app to add nothing", wow.sent[#wow.sent].plain == true and wow.sent[#wow.sent].contextText == nil)
find(function(frame) return frame.label and frame.label._text == "On" and frame._parent == off._parent end):Fire("OnClick")
check("turned back on, the parts are sent again", T.settings.extras and T.Context.Text() ~= nil and option("Character"):IsEnabled())
context:Fire("OnClick")
local scale = find(function(frame) return frame.slider and frame.readout._text == "100%" end)
check("the scale slider shows the saved scale", scale ~= nil)
scale.slider:Fire("OnValueChanged", 111)
check("scale applies at once, on 5% steps", T.settings.scale == 1.1 and scale.readout._text == "110%")
local opacity = find(function(frame) return frame.slider and frame ~= scale end)
opacity.slider:Fire("OnValueChanged", 50)
check("opacity applies at once", T.settings.opacity == 0.5 and find(function(frame) return frame.fill and frame.fill._alpha == 0.5 end) ~= nil)
scale.slider:Fire("OnMouseDown")
scale.slider:Fire("OnValueChanged", 150)
check("dragging scale only updates its readout", T.settings.scale == 1.1 and scale.readout._text == "150%")
scale.slider:Fire("OnMouseUp")
check("releasing scale applies the chosen value", T.settings.scale == 1.5)
opacity.slider:Fire("OnMouseDown")
opacity.slider:Fire("OnValueChanged", 75)
check("opacity also applies during a drag", T.settings.opacity == 0.75)
opacity.slider:Fire("OnMouseUp")
local narrowComposer = T.Composer.Build(CreateFrame("Frame"), {})
narrowComposer:SetWidth(360)
narrowComposer:Set({ context = "Not sending your character", choice = "GPT-6.1-Sol · low · a very long project name", placeholder = "Ask anything" })
check("long composer labels share the available width", narrowComposer.context:GetWidth() + narrowComposer.choice:GetWidth() + 2 * T.Kit.space["space-3"] + T.Kit.space["space-2"] <= narrowComposer:GetWidth() + 0.01)
narrowComposer:Set({ context = "Level 3 Warrior · Shadowglen", about = "GPT-6.1-Sol · medium · 84% context left", placeholder = "Reply" })
narrowComposer:SetWidth(300)
narrowComposer:Fire("OnSizeChanged")
check("resizing keeps the context clear of the model information", narrowComposer.context:GetWidth() + narrowComposer.about:GetWidth() + 2 * T.Kit.space["space-3"] + T.Kit.space["space-2"] <= narrowComposer:GetWidth() + 0.01)
tool("plus"):Fire("OnClick")
local choice = find(function(frame) return frame.glyph and frame.glyph.name == "down" and frame.text and frame.text._text:find("GPT-6.1-Sol", 1, true) end)
check("new conversations show the model", choice ~= nil)
check("the picker replaces the read-only line", not composer.about:IsShown())
choice:Fire("OnClick")
local function offered(label)
    local found = option(label)
    return found ~= nil and found:IsVisible()
end
check("the picker lists only the app's models", offered("GPT-6-Astra") and not offered("Claude Opus"))
check("efforts read as words", offered("Extra high") and offered("Ultra"))
option("GPT-6-Astra"):Fire("OnClick")
check("choosing a model is remembered", T.settings.choice.model == "gpt-6-astra")
option("Ultra"):Fire("OnClick")
option("GPT-6-Astra"):Fire("OnClick")
check("choosing the model already chosen keeps its effort", T.Actions.Choice().effort == "ultra")
find(function(frame) return frame.label and frame.label._text == "Claude Code" and frame._scripts.OnClick end):Fire("OnClick")
check("switching app picks its model", T.settings.choice.model == "claude:opus" and offered("Claude Sonnet") and not offered("GPT-6-Astra"))
local claude = T.Actions.Ask("Why does my bar move in combat?")
check("asking Claude sends a Claude create packet", wow.sent[#wow.sent].model == "claude:opus" and wow.sent[#wow.sent].effort == "high")
T.Window.Select(claude.id)
check("a new Claude conversation says where it is sending", find(function(frame) return frame.text and frame.text._text == "Sending to Claude" and frame:IsVisible() end) ~= nil)

local busy = CopyTable(samples.payloads.ready)
busy.rev = busy.rev + 50
table.insert(busy.threads, { id = "glance-new", agent = "codex", title = "A fresh reply", state = "idle", project = "wow", updated = wow.now, unread = true, turns = {} })
table.insert(busy.threads, { id = "glance-read", agent = "codex", title = "Already read", state = "idle", project = "wow", updated = wow.now, unread = false, turns = {} })
table.insert(busy.threads, { id = "6d2f9b14-0007", agent = "claude", title = claude.title, state = "active", project = claude.project, origin = claude.origin, updated = wow.now, unread = false, turns = {} })
wow.payload = busy
T.Sync.Now()
local glanced = T.Glance.Threads()
local order, rank = true, -1
for index, thread in ipairs(glanced) do
    local this = T.Store.Rank(thread)
    order = order and this >= rank and (this < 2 or T.Store.IsUnread(thread))
    if index > 1 and this == rank then
        order = order and (glanced[index - 1].updated or 0) >= (thread.updated or 0)
    end
    rank = this
end
check("the glance keeps needs you, stopped, working and replied, in that order, newest first", order and glanced[1].id == "019a7c2e-0004" and glanced[2].id == "019a7c2e-0005")
local skipped = true
for _, thread in ipairs(glanced) do
    skipped = skipped and thread.id ~= "glance-read" and thread.state ~= "history"
end
check("the glance leaves out quiet conversations", skipped and T.Store.Find("glance-new") and glanced[#glanced].state == "idle")
tool("minimize"):Fire("OnClick")
local function glance()
    local rows = {}
    for _, frame in ipairs(wow.frames) do
        if frame.word and frame.title and frame:IsVisible() then
            table.insert(rows, frame)
        end
    end
    return rows
end
local shown = glance()
check("minimized shows at most four rows", #shown == 4 and shown[1].word._text == "Needs you" and shown[2].word._text == "Stopped" and shown[3].word._text == "Working")
check("the rest are counted", text((#glanced - 4) .. " more") ~= nil)
local bar = find(function(frame) return frame.mark and frame.heading end)
check("minimized is titled with the mark and thott.bot, with only restore and close", text("thott.bot") ~= nil and bar.mark:IsShown() and not tool("gear"):IsVisible() and not tool("sidebar"):IsVisible() and tool("restore"):IsVisible() and tool("close"):IsVisible())
check("minimized is narrow", T.settings.window.mini and find(function(frame) return frame.grip end):GetWidth() == 280)
shown[2]:Fire("OnClick")
check("a glance row restores the window to it, titled without the mark", not T.settings.window.mini and T.Window.Current() == "019a7c2e-0005" and not bar.mark:IsShown())

T.Window.Select("019a7c2e-0002")
local heading = find(function(frame) return frame._scripts.OnDoubleClick end)
heading:Fire("OnDoubleClick")
local box = find(function(frame) return frame._kind == "EditBox" and frame._scripts.OnEnterPressed and frame:IsShown() and frame._text == "Running out of mana between pulls" end)
check("double-clicking the name edits it", box ~= nil)
local sentBefore = #wow.sent
box:SetText("x")
box:Fire("OnEscapePressed")
check("escape cancels a rename", #wow.sent == sentBefore and not box:IsShown())
heading:Fire("OnDoubleClick")
box:SetText("   ")
box:Fire("OnEnterPressed")
check("an empty name is ignored", #wow.sent == sentBefore)
heading:Fire("OnDoubleClick")
box:SetText("Mana between pulls")
box:Fire("OnEnterPressed")
local renamed = wow.sent[#wow.sent]
check("enter sends a rename packet", renamed.type == "rename" and renamed.thread == "019a7c2e-0002" and renamed.title == "Mana between pulls")
check("the new name shows at once", text("Mana between pulls") ~= nil and T.Store.Title(T.Store.Find("019a7c2e-0002")) == "Mana between pulls")
local confirmed = CopyTable(busy)
confirmed.rev = confirmed.rev + 1
for _, thread in ipairs(confirmed.threads) do
    if thread.id == "019a7c2e-0002" then
        thread.title = "Mana between pulls"
    end
end
wow.payload = confirmed
T.Sync.Now()
check("the helper's title takes over", T.Store.Title(T.Store.Find("019a7c2e-0002")) == "Mana between pulls")

local long = row("Mana between pulls")
long.title.IsTruncated = function() return true end
long:Fire("OnEnter")
check("a cut-off title shows in full on hover", GameTooltip._text == "Mana between pulls")
GameTooltip:SetText("")
local whole = row("Which of my quests are close by?")
whole:Fire("OnEnter")
check("a title that fits has no tooltip", GameTooltip._text == "")

T.Alerts.Tooltip(CreateFrame("Button"))
check("the minimap tooltip says thott.bot chat", GameTooltip._text == "thott.bot chat")
local toc = io.open(here .. "/../thott-bot-chat/thott-bot-chat.toc"):read("*a")
check("the AddOns list says thott.bot chat", toc:find("## Title: thott.bot chat\n", 1, true) ~= nil)

T.Fire("Problem", "low")
T.Fire("Problem", "DISABLED")
check("notices name thott.bot chat", text("thott.bot chat's update channels are turned off in the AddOns list. Turn the thott.bot chat group back on.") ~= nil)

context:Fire("OnClick")
all:Fire("OnClick")
check("None turns every part off", not T.settings.context.character and not T.settings.context.reputation and all.text._text == "All")
check("with every part off nothing is attached", T.Context.Text() == nil)
check("the composer says so", T.Context.Summary() == "Not sending your character")

for _, part in ipairs(T.Context.parts) do
    T.settings.context[part.key] = part.key == "buffs"
end
check("buffs go without reputation", T.Context.Text() == "Game: WoW Forever 1.60.1.70170\nBuffs: Arcane Intellect")
T.settings.context.buffs, T.settings.context.bags = false, true
check("bags go without gear", T.Context.Text() == "Game: WoW Forever 1.60.1.70170\nBags: item:8079 ×25")
T.settings.context.bags, T.settings.context.character, T.settings.context.target = false, true, true
issecretvalue = function(value) return value == 41200 or value == "Elandra" end
check("a value the game keeps secret is left out, never sent as nil", T.Context.Text() == table.concat({
    "Game: WoW Forever 1.60.1.70170",
    "Character: level 33 Human Mage, Alliance · 34g 56s 78c",
    "Target: (npc:2601) level 33, hostile",
}, "\n"))
issecretvalue = function() return false end
T.settings.context.character, T.settings.context.target = false, false

local function exchange(turns, n, at)
    table.insert(turns, { role = "user", text = "question " .. n, at = at })
    table.insert(turns, { role = "activity", text = "Read 1 file", items = {} })
    table.insert(turns, { role = "assistant", text = "answer " .. n, at = at + 5 })
end
local history = {}
for n = 1, 4 do
    exchange(history, n, wow.now - 100 + n * 10)
end
local function windowed(turns)
    local out = {}
    for i = math.max(1, #turns - 11), #turns do
        table.insert(out, turns[i])
    end
    return out
end
local function longThread(rev, state)
    local p = CopyTable(samples.payloads.ready)
    p.rev = p.rev + rev
    table.insert(p.threads, 1, { id = "long", agent = "codex", title = "A long conversation", state = state, project = "wow", updated = p.rev, unread = true, turns = windowed(history) })
    wow.payload = p
    T.Sync.Now()
end
longThread(200, "idle")
longThread(201, "active")
chats = #wow.chat
exchange(history, 5, wow.now)
longThread(202, "idle")
check("a reply alerts once the conversation outgrows the payload's window of turns", #wow.chat == chats + 1 and wow.chat[#wow.chat]:find("Codex replied", 1, true) ~= nil)

tool("plus"):Fire("OnClick")
local lost = T.Actions.Ask("Plan a dungeon run.")
settle()
local unseen = CopyTable(samples.payloads.ready)
unseen.rev, unseen.acks = unseen.rev + 300, { [T.Transport.session] = {} }
wow.payload = unseen
wow.Run(2)
local lostSeq = tonumber(lost.origin:match(":(%d+)$"))
check("a new conversation the app never saw reads as not delivered, no longer working, and stops being waited on", T.Store.Delivery(lostSeq) == "failed" and not T.Store.Tracked(lostSeq) and T.Store.Rank(lost) == 2)
local again = T.Actions.Resend(lost, lostSeq)
local resentCreate = wow.sent[#wow.sent]
check("resending it sends the conversation again, not a reply", again ~= nil and resentCreate.type == "create" and resentCreate.text == "Plan a dungeon run." and lost.origin == T.Transport.session .. ":" .. again)
settle()
local adoptedAgain = CopyTable(unseen)
adoptedAgain.rev, adoptedAgain.acks = adoptedAgain.rev + 1, { [T.Transport.session] = { again } }
table.insert(adoptedAgain.threads, 1, {
    id = "019a7c2e-0007", agent = "codex", title = "Plan a dungeon run.", state = "active", project = "wow", cwd = "~/wow",
    model = "gpt-5.5", effort = "medium", updated = wow.now, since = wow.now, unread = false,
    origin = lost.origin, turns = { { role = "user", text = "Plan a dungeon run.", at = wow.now, context = true } },
})
wow.payload = adoptedAgain
T.Sync.Now()
check("the app's thread replaces the resent conversation", T.Store.Find(lost.id) == nil and T.Store.Find("019a7c2e-0007") ~= nil)

T.Window.Select("019a7c2e-0002")
local lostReply = T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "And after that?")
settle()
T.Actions.Stop(T.Store.Find("019a7c2e-0003"))
local stopSeq = wow.sent[#wow.sent].seq
settle()
local later = CopyTable(adoptedAgain)
later.rev, later.acks = later.rev + 1, { [T.Transport.session] = { stopSeq } }
wow.payload = later
wow.Run(2)
check("a lost reply stays not delivered when a later packet is acked", T.Store.Delivery(lostReply) == "failed" and T.Store.Delivery(stopSeq) == "delivered")
local late = CopyTable(later)
late.rev, late.acks = late.rev + 1, { [T.Transport.session] = { stopSeq, lostReply } }
wow.payload = late
T.Sync.Now()
check("a late ack still delivers it", T.Store.Delivery(lostReply) == "delivered")

local sentAt
T.On("PacketSent", function() sentAt = GetTime() end)
local loads = #wow.loaded
T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Still there?")
settle()
wow.Run(60)
local gaps, last, cadence = { 2, 3, 5, 8, 13, 13, 13 }, sentAt, #wow.loaded == loads + 7
for index = loads + 1, #wow.loaded do
    cadence = cadence and wow.loaded[index].at - last == gaps[index - loads]
    last = wow.loaded[index].at
end
check("while a conversation is working, checks follow the frames by 2 s, then come 3, 5, 8 and 13 s apart, then every 13 s", cadence)

local quiet = CopyTable(late)
quiet.rev = quiet.rev + 1
for _, thread in ipairs(quiet.threads) do
    if T.Store.Rank(thread) == 1 then
        thread.state = "idle"
    end
end
wow.payload = quiet
local unheard = T.Actions.Reply(T.Store.Find("019a7c2e-0002"), "Hello?")
settle()
wow.Run(2)
check("a message never acked stops being waited on once it reads as not delivered", T.Store.Delivery(unheard) == "failed" and not T.Store.Tracked(unheard) and not T.Store.Awaiting())
local left = T.Transport.SlotsLeft()
loads = #wow.loaded
wow.Run(120)
check("with nothing else awaited the checks stop, spending no more of the session's 128 loads", #wow.loaded == loads and T.Transport.SlotsLeft() == left)

local function received()
    local packets, stream, count = {}, nil, nil
    for _, frame in ipairs(wow.bits) do
        local head = frame:byte(1)
        if head >= 128 then
            stream = ""
        elseif head ~= ((count or 0) + 1) % 128 then
            stream = nil
        end
        count = head % 128
        stream = stream and stream .. frame:sub(2)
        local length = stream and #stream >= 2 and stream:byte(1) * 256 + stream:byte(2)
        if length and #stream >= 2 + length then
            table.insert(packets, { body = stream:sub(3, 2 + length), padding = stream:sub(3 + length) })
            stream = nil
        end
    end
    return packets
end
local function same(a, b)
    for key, value in pairs(a) do
        if b[key] ~= value then
            return false
        end
    end
    for key in pairs(b) do
        if a[key] == nil then
            return false
        end
    end
    return true
end
local packets = received()
local intact = #packets == #wow.sent
for index, packet in ipairs(wow.sent) do
    local got = packets[index]
    intact = intact and got ~= nil and got.padding == string.rep("\0", #got.padding) and same(packet, wow.Decode(got.body))
end
check("every packet reads back from the saved frames: a first-frame bit, a counter, a 2-byte length, the JSON, zeros", intact)

local old = CopyTable(samples.payloads.ready)
old.v, old.rev = 3, old.rev + 400
wow.payload = old
T.Sync.Now()
check("a payload from another protocol version asks to restart WoW, since the app installs its own addon", text("Restart WoW to finish setup.") ~= nil)

local link = EventRegistry.callbacks.SetItemRef
link("addon:thott-bot-chat:open:019a7c2e-0005")
check("a chat link opens its thread", T.Window.Current() == "019a7c2e-0005")

local signedOut = CopyTable(samples.payloads.ready)
signedOut.rev, signedOut.codex, signedOut.note = signedOut.rev + 99, "signed-out", "Sign in to the Codex CLI on this computer."
wow.payload = signedOut
T.Sync.Now()
check("the helper's reason shows when Codex isn't ready", find(function(frame) return frame.text and frame.text._text == signedOut.note end) ~= nil)

local away = CopyTable(samples.payloads.offline)
away.rev = signedOut.rev + 1
wow.payload = away
T.Sync.Now()
check("with the app away, the window asks for the app and the CLI it needs, and offers Try again",
    text("Connect your AI") and text("Start the thott.bot chat app on this computer. The app connects to the Codex CLI or the Claude Code CLI.") and key("Try again"):IsVisible())
local restart = CopyTable(samples.payloads.restart)
restart.rev = away.rev + 1
wow.payload = restart
T.Sync.Now()
check("an addon older than the app asks for a WoW restart instead of looking connected",
    text("Restart WoW") and text("The thott.bot chat app updates the addon when WoW closes.") and text("Connect your AI") == nil
    and not key("Try again"):IsVisible() and not find(function(frame) return frame.about end):IsVisible())
local back = CopyTable(signedOut)
back.rev, back.codex, back.note = restart.rev + 1, "ready", ""
wow.payload = back
T.Sync.Now()

local linked = T.Format.Blocks("Train [Mage Armor](spell:6117), keep your [Hearthstone](item:6948), not [Frost Armor](spell:6117) or [Thing](item:999).", "ffffffff")[1].text
check("names the game confirms become its own links", linked:find("|Hspell:6117:0|h[Mage Armor]|h", 1, true) and linked:find("|Hitem:6948", 1, true))
check("names the game doesn't confirm stay plain text", linked:find("not Frost Armor or Thing.", 1, true) and wow.requested == 999)
wow.requested = nil
wow.Event("ITEM_DATA_LOAD_RESULT", 999, false)
T.Format.Blocks("Keep [Thing](item:999).", "ffffffff")
check("an id the game says doesn't exist is asked for once", wow.requested == nil)

local transcript = T.Transcript.Build(CreateFrame("Frame"), {})
local range = 200
function transcript:GetVerticalScrollRange() return range end
local scrolling = { id = "scroll-test", state = "idle", turns = { { role = "assistant", text = "A reply." } } }
transcript:Render(scrolling, wow.now)
wow.Run(0)
range = 260
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("following replies survives a layout range change", transcript.target == range)
transcript:Fire("OnMouseWheel", 1)
local readingAt = transcript.target
range = 310
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("a new reply preserves manual scrolling", transcript.target == readingAt)
transcript:ScrollToEnd()
range = 360
transcript:Render(scrolling, wow.now)
wow.Run(0)
check("returning to the bottom follows replies again", transcript.target == range)
transcript:Fire("OnMouseWheel", 1)
for _ = 1, 30 do
    wow.Run(1 / 60)
end
check("the wheel glides the transcript to where it sends it", transcript:GetVerticalScroll() == transcript.target and transcript.target < range)
range = 0
transcript:ScrollTo(0)
for _ = 1, 30 do
    wow.Run(1 / 60)
end
check("with nothing left to scroll, the transcript stops its per-frame work", transcript:GetScript("OnUpdate") == nil)

local labelled = CopyTable(samples.payloads.ready)
labelled.rev = labelled.rev + 600
for _, model in ipairs(labelled.models) do
    model.label = model.label .. " |n"
end
wow.payload = labelled
T.Sync.Now()
tool("plus"):Fire("OnClick")
check("the composer shows a model's name as the app sent it, as the picker does", choice.text._text:find(" ||n · ", 1, true) ~= nil)
local named = T.Actions.Ask(string.rep("é", 130))
check("a long question names its conversation by the first 120 characters, never half of one", named.title == string.rep("é", 120))

local function toasts(id)
    local count = 0
    for _, frame in ipairs(wow.frames) do
        if frame.stripe and frame.close and frame:IsShown() and frame.id == id then
            count = count + 1
        end
    end
    return count
end
local pulls, quests = T.Store.Find("019a7c2e-0002"), T.Store.Find("019a7c2e-0003")
T.Fire("StoreChanged", { replied = { pulls } })
T.Fire("StoreChanged", { replied = { quests } })
find(function(frame) return frame.stripe and frame.close and frame:IsShown() and frame.id == pulls.id end):Fire("OnClick")
T.Fire("StoreChanged", { replied = { quests } })
check("another reply in a conversation renews its toast instead of stacking a second", toasts(quests.id) == 1)

print(string.format("ok %d checks", passed))
