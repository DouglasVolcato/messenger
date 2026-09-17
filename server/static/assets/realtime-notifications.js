(() => {
  const reconnectDelayMin = 1000;
  const reconnectDelayMax = 10000;
  const toastDuration = 8000;
  let reconnectDelay = reconnectDelayMin;
  let socket;
  let stopped = false;

  const typeLabels = {
    DIRECT_MESSAGE: "Direct message",
    CHAT_MESSAGE: "Chat message",
    COMPANY_MEMBERSHIP: "Company membership",
  };

  function toastContainer() {
    let container = document.querySelector("[data-notification-toasts]");
    if (container) return container;

    container = document.createElement("section");
    container.className = "notification-toasts";
    container.dataset.notificationToasts = "";
    container.setAttribute("aria-live", "polite");
    container.setAttribute("aria-label", "Notifications");
    document.body.append(container);
    return container;
  }

  function safeActionURL(value) {
    if (!value) return null;
    try {
      const url = new URL(value, window.location.origin);
      return url.origin === window.location.origin ? url.href : null;
    } catch {
      return null;
    }
  }

  function showToast(notification) {
    const toast = document.createElement("article");
    toast.className = "notification-toast";

    const header = document.createElement("div");
    header.className = "notification-toast-header";

    const heading = document.createElement("div");
    const type = document.createElement("p");
    type.className = "notification-toast-type";
    type.textContent = typeLabels[notification.type] || notification.type || "Notification";
    heading.append(type);

    if (notification.title) {
      const title = document.createElement("strong");
      title.textContent = notification.title;
      heading.append(title);
    }

    const close = document.createElement("button");
    close.type = "button";
    close.className = "notification-toast-close";
    close.textContent = "×";
    close.setAttribute("aria-label", "Dismiss notification");
    close.addEventListener("click", () => toast.remove());

    header.append(heading, close);
    toast.append(header);

    if (notification.content) {
      const content = document.createElement("p");
      content.className = "mt-2 text-sm";
      content.textContent = notification.content;
      toast.append(content);
    }

    const actionURL = safeActionURL(notification.action_url);
    if (actionURL) {
      const link = document.createElement("a");
      link.className = "notification-toast-link";
      link.href = actionURL;
      link.textContent = "Open notification";
      link.addEventListener("click", (event) => {
        const action = new URL(actionURL);
        if (action.pathname !== window.location.pathname || action.search !== window.location.search) return;

        event.preventDefault();
        window.history.replaceState(null, "", `${action.pathname}${action.search}${action.hash}`);
        window.location.reload();
      });
      toast.append(link);
    }

    toastContainer().prepend(toast);
    window.setTimeout(() => toast.remove(), toastDuration);
  }

  function websocketURL() {
    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${scheme}//${window.location.host}/ws/notifications`;
  }

  function connect() {
    if (stopped) return;
    socket = new WebSocket(websocketURL());

    socket.addEventListener("open", () => {
      reconnectDelay = reconnectDelayMin;
    });

    socket.addEventListener("message", (event) => {
      try {
        showToast(JSON.parse(event.data));
      } catch {
        console.warn("Received an invalid realtime notification");
      }
    });

    socket.addEventListener("close", (event) => {
      if (stopped || event.code === 1008) return;
      window.setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, reconnectDelayMax);
    });

    socket.addEventListener("error", () => socket.close());
  }

  window.addEventListener("pagehide", () => {
    stopped = true;
    if (socket) socket.close();
  });

  connect();
})();
