local _, T = ...

local Actions = {}
T.Actions = Actions

local function send(packet)
    local seq, reason = T.Transport.Send(packet)
    if seq then
        T.Store.Track(seq)
    else
        T.Fire("Problem", reason)
    end
    return seq
end

local function find(list, key, value)
    for _, item in ipairs(list) do
        if item[key] == value then
            return item
        end
    end
end

function Actions.Agent(model)
    return model.id:find("^claude:") and "claude" or "codex"
end

function Actions.Choice()
    local saved, models = T.settings.choice, T.Store.models
    local model = find(models, "id", saved.model) or find(models, "default", true) or models[1]
    local effort = model and (tContains(model.efforts or {}, saved.effort) and saved.effort or model.effort)
    return {
        model = model,
        effort = effort,
        project = find(T.Store.projects, "cwd", saved.cwd) or T.Store.projects[1],
    }
end

function Actions.Choose(key, value)
    local model = Actions.Choice().model
    T.settings.choice[key] = value
    if key == "model" and (not model or model.id ~= value) then
        T.settings.choice.effort = nil
    end
    T.Fire("StoreChanged", {})
end

local function message(packet)
    packet.contextText = T.Context.Text()
    packet.plain = not T.settings.extras or nil
    return send(packet)
end

function Actions.Ask(text)
    local choice = Actions.Choice()
    if not choice.model or not choice.project then
        T.Fire("Problem", "unready")
        return
    end
    local seq = message({ type = "create", text = text, cwd = choice.project.cwd, model = choice.model.id, effort = choice.effort })
    return seq and T.Store.AddTask({
        text = text, cwd = choice.project.cwd, project = choice.project.label,
        model = choice.model.id, effort = choice.effort, agent = Actions.Agent(choice.model),
    }, seq)
end

function Actions.Reply(thread, text)
    local seq = message({ type = "reply", thread = thread.id, text = text })
    if seq then
        T.Store.AddReply(thread, text, seq)
    end
    return seq
end

function Actions.Resend(thread, seq)
    local text = T.Store.Retract(thread, seq)
    if not text then
        return
    end
    if thread.id:find("^local:") then
        local again = message({ type = "create", text = text, cwd = thread.cwd, model = thread.model, effort = thread.effort })
        if again then
            T.Store.Reissue(thread, text, again)
        end
        return again
    end
    return Actions.Reply(thread, text)
end

function Actions.Rename(thread, title)
    if send({ type = "rename", thread = thread.id, title = title }) then
        T.Store.Rename(thread, title)
    end
end

function Actions.Answer(thread, allow)
    local approval = thread.approval
    if approval and send({ type = allow and "approve" or "deny", thread = thread.id, approval = approval.id }) then
        T.Store.Answer(thread, approval.id)
    end
end

function Actions.Stop(thread)
    send({ type = "stop", thread = thread.id })
end

function Actions.Read(thread)
    if T.Store.MarkRead(thread) then
        T.Transport.Send({ type = "read", thread = thread.id })
    end
end
