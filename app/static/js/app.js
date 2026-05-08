// SMS Mock Server - UI Functions

// Function to open modal
function openModal() {
    document.getElementById('modal').classList.add('show');
}

// Function to close modal
function closeModal() {
    document.getElementById('modal').classList.remove('show');
}

// Function to load and show message detail
function showMessageDetail(messageSid) {
    var url = '/ui/fragments/message/' + messageSid;

    fetch(url)
        .then(function(response) {
            if (!response.ok) {
                throw new Error('HTTP ' + response.status + ': ' + response.statusText);
            }
            return response.text();
        })
        .then(function(html) {
            document.getElementById('modal-content').innerHTML = html;
            document.getElementById('modal').classList.add('show');
        })
        .catch(function(error) {
            alert('Error loading message details: ' + error.message);
        });
}

// Function to load and show call detail
function showCallDetail(callSid) {
    var url = '/ui/fragments/call/' + callSid;

    fetch(url)
        .then(function(response) {
            if (!response.ok) {
                throw new Error('HTTP ' + response.status + ': ' + response.statusText);
            }
            return response.text();
        })
        .then(function(html) {
            document.getElementById('modal-content').innerHTML = html;
            document.getElementById('modal').classList.add('show');
        })
        .catch(function(error) {
            alert('Error loading call details: ' + error.message);
        });
}
