const state = {
  profile: null,
  changeAccount: false,
  currentPage: "account",
  running: false,
  completed: false,
  phases: {
    account: { state: "idle", message: "This step is skipped unless you choose to change the account." },
    "first-run": { state: "idle", message: "The mandatory setup has not started yet." },
    upgrade: { state: "idle", message: "Caracal update has not started yet." },
    finish: { state: "idle", message: "Reboot becomes available after setup finishes." },
  },
};

const elements = {
  stepItems: [...document.querySelectorAll(".step-chip")],
  progressCards: [...document.querySelectorAll(".progress-item")],
  pages: {
    account: document.querySelector("#page-account"),
    "first-run": document.querySelector("#page-first-run"),
    finish: document.querySelector("#page-finish"),
  },
  currentUsername: document.querySelector("#current-username"),
  currentHome: document.querySelector("#current-home"),
  skipChoice: document.querySelector("#skip-choice"),
  changeChoice: document.querySelector("#change-choice"),
  usernameInput: document.querySelector("#username-input"),
  hostnameInput: document.querySelector("#hostname-input"),
  passwordInput: document.querySelector("#password-input"),
  confirmPasswordInput: document.querySelector("#confirm-password-input"),
  accountHint: document.querySelector("#account-hint"),
  saveButton: document.querySelector("#save-button"),
  upgradeButton: document.querySelector("#upgrade-button"),
  nextButton: document.querySelector("#next-button"),
  rebootButton: document.querySelector("#reboot-button"),
  finishSummary: document.querySelector("#finish-summary"),
  log: document.querySelector("#log"),
  runPill: document.querySelector("#run-pill"),
};

function backend() {
  const bound = window.go?.guiapp?.App || window.go?.main?.App;
  if (!bound) {
    throw new Error("Wails backend bindings are not available.");
  }
  return bound;
}

async function boot() {
  bindEvents();
  bindRuntimeEvents();
  await loadProfile();
  render();
  appendLog("Wizard ready.");
}

function bindEvents() {
  elements.skipChoice.addEventListener("click", () => {
    state.changeAccount = false;
    render();
  });

  elements.changeChoice.addEventListener("click", () => {
    state.changeAccount = true;
    render();
    elements.usernameInput.focus();
  });

  elements.saveButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    const request = buildRequest();
    if (!request) {
      return;
    }

    state.running = true;
    state.completed = false;
    state.phases.account = {
      state: "idle",
      message: "Saving requested account and hostname changes.",
    };
    state.phases["first-run"] = {
      state: "idle",
      message: "First-run setup was not requested.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Save is in progress.",
    };
    render();

    appendLog("Saving account and hostname details...");

    try {
      const result = await backend().SaveDetails(request);
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = result.rebootRequired
        ? `Saved details for ${result.appliedUsername} on ${result.appliedHostname}. Reboot or sign out to fully apply account and hostname changes.`
        : "No changes were saved because the current details already match.";
      await loadProfile();
      appendLog("Details saved.");
      render();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.nextButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    const request = buildRequest({ allowNoChanges: true });
    if (!request) {
      return;
    }

    state.currentPage = "first-run";
    state.running = true;
    state.completed = false;
    state.phases["first-run"] = {
      state: "idle",
      message: "The terminal window will open once the account step is done.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Reboot will unlock after the mandatory setup finishes.",
    };
    render();

    appendLog(request.changeAccount || request.changeHostname ? "Saving details before first-run setup..." : "Skipping detail changes.");

    try {
      const result = await backend().RunSetup(request);
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `First-run setup completed for ${result.appliedUsername} on ${result.appliedHostname}. Reboot now to finish applying the Caracal session changes.`;
      await loadProfile();
      appendLog("Mandatory setup finished. Reboot is ready.");
      render();
    } catch (error) {
      state.running = false;
      if (state.phases.account.state === "error" && state.phases["first-run"].state !== "complete") {
        state.currentPage = "account";
      }
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.upgradeButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    state.currentPage = "first-run";
    state.running = true;
    state.completed = false;
    state.phases.account = {
      state: "idle",
      message: "Account and hostname details are unchanged.",
    };
    state.phases["first-run"] = {
      state: "idle",
      message: "First-run setup was not requested.",
    };
    state.phases.upgrade = {
      state: "idle",
      message: "The terminal window will open for the Caracal update.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Reboot will be available after the update finishes.",
    };
    render();

    appendLog("Starting Caracal update...");

    try {
      const result = await backend().RunUpgrade();
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `Caracal update completed for ${result.appliedUsername} on ${result.appliedHostname}. Reboot if the updater requested it.`;
      await loadProfile();
      appendLog("Caracal update finished.");
      render();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.rebootButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    state.running = true;
    render();
    appendLog("Requesting reboot...");

    try {
      await backend().RebootNow();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });
}

function bindRuntimeEvents() {
  if (!window.runtime?.EventsOn) {
    return;
  }

  window.runtime.EventsOn("setup:phase", (payload) => {
    state.phases[payload.id] = {
      state: payload.state,
      message: payload.message,
    };

    if (payload.id === "first-run" && payload.state === "running") {
      appendLog("Launching a terminal window for ujust first-run.");
      appendLog("Complete the prompts there, then return here when it closes.");
    } else if (payload.id === "upgrade" && payload.state === "running") {
      appendLog("Launching a terminal window for ujust upgrade.");
      appendLog("Complete the prompts there, then return here when it closes.");
    } else {
      appendLog(payload.message);
    }

    render();
  });
}

async function loadProfile() {
  const profile = await backend().GetProfile();
  state.profile = profile;
  elements.currentUsername.textContent = profile.currentUsername || "Unknown user";
  elements.usernameInput.value = profile.currentUsername || "";
  elements.hostnameInput.value = "";
  elements.hostnameInput.placeholder = profile.currentHostname || "Current hostname";
  elements.currentHome.textContent = profile.currentHome ? `• ${profile.currentHome}` : "";
}

function buildRequest(options = {}) {
  const username = (elements.usernameInput.value || "").trim();
  const hostname = (elements.hostnameInput.value || "").trim();
  const password = elements.passwordInput.value || "";
  const confirmPassword = elements.confirmPasswordInput.value || "";

  const usernameChanged = username && username !== (state.profile?.currentUsername || "");
  const passwordChanged = password !== "";
  const hostnameChanged = hostname && hostname !== (state.profile?.currentHostname || "");
  const changeAccount = state.changeAccount && (usernameChanged || passwordChanged);

  if (state.changeAccount && !username) {
    appendLog("Enter a username or keep the current account details.");
    return null;
  }

  if (password !== confirmPassword) {
    appendLog("Password confirmation does not match.");
    return null;
  }

  if (!options.allowNoChanges && !changeAccount && !hostnameChanged) {
    appendLog("Change the username, password, or hostname before saving.");
    return null;
  }

  return {
    changeAccount,
    changeHostname: hostnameChanged,
    username: username || state.profile?.currentUsername || "",
    password,
    hostname: hostname || state.profile?.currentHostname || "",
  };
}

function render() {
  elements.skipChoice.classList.toggle("is-active", !state.changeAccount);
  elements.changeChoice.classList.toggle("is-active", state.changeAccount);
  elements.skipChoice.setAttribute("aria-pressed", String(!state.changeAccount));
  elements.changeChoice.setAttribute("aria-pressed", String(state.changeAccount));

  const disableForm = state.running || !state.changeAccount;
  elements.usernameInput.disabled = disableForm;
  elements.passwordInput.disabled = disableForm;
  elements.confirmPasswordInput.disabled = disableForm;
  elements.hostnameInput.disabled = state.running;
  elements.accountHint.textContent = state.changeAccount
    ? "Update any account detail you want to change. Leave the password blank to keep it unchanged."
    : "Keeping account details still lets you update the hostname.";

  elements.saveButton.disabled = state.running;
  elements.upgradeButton.disabled = state.running;
  elements.nextButton.disabled = state.running;
  elements.saveButton.textContent = state.running ? "Saving..." : "Save Details";
  elements.upgradeButton.textContent = state.running ? "Updating..." : "Update Caracal";
  elements.nextButton.textContent = state.running ? "Running Setup..." : "Save and Run First-Run";
  elements.rebootButton.disabled = state.running;

  updatePill(elements.runPill, state.running ? "running" : state.completed ? "success" : "neutral");
  elements.runPill.textContent = state.running ? "Running" : state.completed ? "Complete" : "Idle";

  const activeStep = state.currentPage;

  for (const [key, page] of Object.entries(elements.pages)) {
    page.classList.toggle("is-hidden", key !== state.currentPage);
  }

  for (const item of elements.stepItems) {
    const key = item.dataset.step;
    const phase = state.phases[key] || { state: "idle" };
    item.classList.toggle("is-active", key === activeStep);
    item.classList.toggle("is-complete", phase.state === "complete" || phase.state === "ready");
    item.classList.toggle("is-error", phase.state === "error");
  }

  for (const card of elements.progressCards) {
    const key = card.dataset.progress;
    const phase = state.phases[key] || { state: "idle", message: "" };
    card.classList.toggle("is-running", phase.state === "running");
    card.classList.toggle("is-complete", phase.state === "complete");
    card.classList.toggle("is-ready", phase.state === "ready");
    card.classList.toggle("is-error", phase.state === "error");
    const copy = card.querySelector(".progress-copy");
    if (copy && phase.message) {
      copy.textContent = phase.message;
    }
  }
}

function updatePill(element, stateName) {
  element.classList.remove("neutral", "running", "success", "error");
  switch (stateName) {
    case "running":
      element.classList.add("running");
      break;
    case "complete":
    case "ready":
    case "success":
      element.classList.add("success");
      break;
    case "error":
      element.classList.add("error");
      break;
    default:
      element.classList.add("neutral");
      break;
  }
}

function appendLog(message) {
  const line = `[${new Date().toLocaleTimeString()}] ${message}`;
  elements.log.textContent = elements.log.textContent
    ? `${elements.log.textContent}\n${line}`
    : line;
  elements.log.scrollTop = elements.log.scrollHeight;
}

boot().catch((error) => {
  appendLog(error?.message || String(error));
});
