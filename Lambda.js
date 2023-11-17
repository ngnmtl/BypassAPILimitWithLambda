const https = require('https');

exports.handler = async (event) => {
  try {
    const urlQueryParam = event.queryStringParameters.url;
    if (!urlQueryParam) { return { statusCode: 400, body: JSON.stringify({ error: 'URL parameter is missing' }), headers: { 'Content-Type': 'application/json' } }; }
    const baseUrl = urlQueryParam.includes('data/v4') ? 'https://open.example.com/' : 'https://api.example.com/';
    const fullUrl = urlQueryParam.includes('https://') ? decodeURIComponent(urlQueryParam) : `${baseUrl}${urlQueryParam}`;
    const headers = {};
    const options = { method: 'GET', headers };
    const response = await new Promise((resolve, reject) => {
      const req = https.request(fullUrl, options, (res) => {
        let data = '';
        res.on('data', (chunk) => data += chunk);
        res.on('end', () => resolve({ statusCode: res.statusCode, body: data, headers: { 'Content-Type': 'application/json' } }));
      });
      req.on('error', (error) => reject(error));
      req.end();
    });
    return response;
  } catch (error) {
    return { statusCode: error.response ? error.response.status : 500, body: JSON.stringify({ error }), headers: { 'Content-Type': 'application/json' } };
  }
};
