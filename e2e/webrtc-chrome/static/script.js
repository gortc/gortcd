var peerConnection;
var peerConnectionConfig = {
    'iceServers': [
        {
            'url': 'stun:turn-server',
        },
        {
            'url': 'turn:turn-server',
            'username': 'username',
            'credential': 'secret'
        }
    ],
    'iceTransportPolicy': 'relay'
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
        } else {
            serverConnection.onopen = () => {
                fetch("/initialized", {
                    method: "post"
                }).catch((reason) => {
                    console.log("failed to init notify", reason)
                })
            }
        }
    });
}

function receiveChannelCallback(event) {
    console.log('received data channel');
    const receiveChannel = event.channel;
    receiveChannel.onmessage = (event) => {
        console.log("dataChannel message:", event.data);
        fetch("/success").then(function () {
            console.log("success");
        })
    };
    receiveChannel.onopen = () => {
        receiveChannel.send("hello [from controlled]");
    };
    receiveChannel.onclose = () => {
        console.log("dataChannel closed");
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
            dataChannel.send("hello [from caller]");
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