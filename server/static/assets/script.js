htmx.config.responseHandling.alwaysTriggerOnEvents = true;
document.addEventListener("htmx:responseError", function(event) {
    const hxTriggerHeader = event.detail.xhr.getResponseHeader("HX-Trigger");
    if (hxTriggerHeader) {
        const triggerData = JSON.parse(hxTriggerHeader);
        if (triggerData.errorMessage) {
            alert(triggerData.errorMessage.message);
        }
    }
});
document.addEventListener("htmx:errorMessage", function (event) {
    const mensagem = event.detail.message;
    alert(mensagem);
});