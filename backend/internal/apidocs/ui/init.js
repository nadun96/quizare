// Swagger UI bootstrap. Requests from "Try it out" carry the session cookie and
// the CSRF header the API requires on state-changing calls (ADR-13).
window.addEventListener('load', function () {
	window.ui = SwaggerUIBundle({
		url: '/api/docs/openapi.yaml',
		dom_id: '#swagger-ui',
		deepLinking: true,
		persistAuthorization: false,
		docExpansion: 'none',
		filter: true,
		tryItOutEnabled: false,
		requestInterceptor: function (req) {
			req.headers['X-Requested-With'] = 'swagger-ui';
			req.credentials = 'same-origin';
			return req;
		}
	});
});
