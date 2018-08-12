var peerConnection;
var peerConnectionConfig = {
    'iceServers': [
        {
            'url': 'stun:turn-server',
            'username': 'username',
            'credential': 'secret'
        }
    ],
    'iceTransportPolicy': 'all'
};
const dataChannelOptions = {
    ordered: false, // do not guarantee order
};

function pageReady() {
    fetch("/config").then(res => res.json()).then(function (myJSON) {
        serverConnection = new WebSocket(myJSON.signaling);
        serverConnection.onmessage = gotMessageFromServer;

        if (myJSON.controlling) {
            start(true);
        }
    });
}

function receiveChannelCallback(event) {
    console.log('Receive Channel Callback');
    const receiveChannel = event.channel;
    receiveChannel.onmessage = (event) => {
        console.log("Got Data Channel Message:", event.data);
    };
    receiveChannel.onopen = () => {
        receiveChannel.send("Hello World!");
    };
    receiveChannel.onclose = () => {
        console.log("The Data Channel is Closed");
    };
}

function start(isCaller) {
    peerConnection = new RTCPeerConnection(peerConnectionConfig);
    peerConnection.onicecandidate = gotIceCandidate;
    peerConnection.ondatachannel = receiveChannelCallback;
    if(isCaller) {
        const dataChannel = peerConnection.createDataChannel("matrix", dataChannelOptions);
        dataChannel.onerror = (error) => {
            console.log("dataChannel error:", error);
        };
        dataChannel.onmessage = (event) => {
            console.log("dataChannel message:", event.data);
            fetch("/success").then(function () {
                console.log("success");
            })
        };
        dataChannel.onopen = () => {
            dataChannel.send("hello");
        };
        dataChannel.onclose = () => {
            console.log("dataChannel closed");
        };
        peerConnection.createOffer().then(gotDescription).catch(createOfferError);
    }
}

function gotDescription(description) {
    console.log('got description');
    peerConnection.setLocalDescription(description).then(function () {
        serverConnection.send(JSON.stringify({'sdp': description}))
    }).catch(function () {
        console.log('set description error')
    });
}

function gotIceCandidate(event) {
    if(event.candidate != null) {
        serverConnection.send(JSON.stringify({'ice': event.candidate}));
    }
}

function createOfferError(error) {
    console.log(error);
}

function gotMessageFromServer(message) {
    if(!peerConnection) start(false);
    const signal = JSON.parse(message.data);
    if(signal.sdp) {
        peerConnection.setRemoteDescription(new RTCSessionDescription(signal.sdp)).then(function() {
            if(signal.sdp.type === 'offer') {
                peerConnection.createAnswer().then(gotDescription).catch(function (err) {
                    console.log(err);
                });
            }
        });
    } else if(signal.ice) {
        peerConnection.addIceCandidate(new RTCIceCandidate(signal.ice)).then(function () {
            console.log("ice candidate added")
        });
    }
}

pageReady();