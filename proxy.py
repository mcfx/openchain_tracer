from flask import Flask, jsonify
import requests

erigon_url = 'http://127.0.0.1:8545'

app = Flask(__name__)


@app.route('/api/v1/trace/ethereum/<txh>')
def trace(txh):
    res = requests.post(erigon_url, json={
        'jsonrpc': '2.0',
        'id': 1,
        'method': 'debug_traceTransaction',
        'params': [
            txh,
            {'tracer': 'openchainTracer'},
        ]
    }).json()
    resp = jsonify({
        'ok': True,
        'result': {
            'chain': 'ethereum',
            'entrypoint': res['result']['entrypoint'],
            'preimages': res['result']['preimages'],
            'addresses': res['result']['addresses'],
            'txhash': txh
        }
    })
    resp.headers['Access-Control-Allow-Origin'] = '*'
    return resp


app.run(host='0.0.0.0', port=2000)
