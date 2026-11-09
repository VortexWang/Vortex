#!/usr/bin/env ruby
require 'yaml'
def check(value, message)
  raise message unless value
end
begin
  doc = YAML.safe_load(File.read(ARGV.fetch(0)), permitted_classes: [], aliases: false)
  check(doc.is_a?(Hash) && doc['openapi'] == '3.1.1', 'expected OpenAPI 3.1.1')
  check(doc.dig('components', 'securitySchemes', 'bearerAuth', 'scheme') == 'bearer', 'bearer scheme missing')
  check(doc['security'] == [{'bearerAuth' => []}], 'global authentication required')
  resolve = lambda do |v|
    next v unless v.is_a?(Hash) && v.key?('$ref')
    ref = v['$ref']
    check(ref.is_a?(String) && ref.start_with?('#/components/'), 'external reference forbidden')
    found = ref.delete_prefix('#/').split('/').reduce(doc) { |h, k| h.is_a?(Hash) ? h[k] : nil }
    check(found.is_a?(Hash), "unresolved reference #{ref}")
    found
  end
  visit = lambda do |v|
    if v.is_a?(Hash)
      resolve.call(v) if v.key?('$ref')
      if v['properties']
        check(v['additionalProperties'] == false, 'public objects must exclude undeclared fields')
        v['properties'].each_key do |name|
          # A rule's user-authored destination list is business input. Keep
          # runtime/transport target fields forbidden everywhere else.
          rule_destination = name == 'targets' && v.equal?(doc.dig('components', 'schemas', 'RuleInput'))
          business_revision = name == 'target_revision' && v.equal?(doc.dig('components', 'schemas', 'PublicOperation'))
          check(rule_destination || business_revision || !name.match?(/password|secret|private_key|credential|runtime|vlink|wire|payload|target|boot_id|signer_epoch/i), "internal/secret property #{name}")
        end
      end
      v.each_value { |x| visit.call(x) }
    elsif v.is_a?(Array)
      v.each { |x| visit.call(x) }
    end
  end
  visit.call(doc)
  doc.fetch('components').fetch('schemas').each_key do |name|
    check(!name.match?(/secret|credential|runtime|vlink|wire|privatekey/i), "internal schema #{name}")
  end
  error = doc.dig('components', 'schemas', 'ApiError')
  check(error && %w[code message request_id].all? { |k| error.fetch('required', []).include?(k) }, 'stable error envelope missing')
  paths = doc['paths']
  check(paths.is_a?(Hash) && !paths.empty?, 'paths must not be empty')
  ids = []
  paths.each do |path, item|
    check(path.start_with?('/api/v1/'), "unversioned path #{path}")
    check(!path.match?(/runtime|vlink|private-key|credential|wire/i), "internal route #{path}")
    item.each do |method, op|
      next if method == 'parameters' || method.start_with?('x-')
      check(%w[get post put patch delete].include?(method), "unsupported method #{method}")
      id = op['operationId']
      check(id.is_a?(String) && !id.empty? && !ids.include?(id), 'operationId must be unique')
      ids << id
      refresh = path == '/api/v1/auth/refresh' && method == 'post'
      if refresh
        check(op['security'] == [] && op['x-vortex-refresh-auth'] == 'rotating-body-token' && op.dig('requestBody', 'content', 'application/json', 'schema', '$ref') == '#/components/schemas/RefreshRequest', 'refresh proof missing')
        request = doc.dig('components', 'schemas', 'RefreshRequest')
        check(request && request['required'] == ['refresh_token'] && request.dig('properties', 'refresh_token', 'writeOnly') == true, 'refresh body token required')
      else
        check(!op.key?('security') || op['security'] == doc['security'], "authentication bypass #{id}")
      end
      params = (item.fetch('parameters', []) + op.fetch('parameters', [])).map { |p| resolve.call(p) }
      path.scan(/\{([^}]+)\}/).flatten.each do |name|
        check(params.any? { |p| p['name'] == name && p['in'] == 'path' && p['required'] == true }, "missing path parameter #{name}")
      end
      params.select { |p| p['name'] == 'If-Match' }.each do |p|
        check(p['in'] == 'header' && p['required'] == true && p.dig('schema', 'pattern').to_s.start_with?('^"') && p.dig('schema', 'pattern').to_s.end_with?('"$'), 'strong quoted If-Match required') if doc.dig('info', 'version').to_s.start_with?('3.')
        check(op.fetch('responses').key?('412') && op.fetch('responses').key?('428'), 'CAS errors missing') if doc.dig('info', 'version').to_s.start_with?('3.')
      end
      responses = op.fetch('responses')
      check(responses.keys.any? { |s| s.to_s.start_with?('2') }, "success response missing #{id}")
      modern = doc.dig('info', 'version').to_s.split('.').first.to_i >= 3
      required_errors = modern ? (refresh ? %w[400 401 415 500 503] : (path == '/api/v1/auth/logout' ? %w[401 500 503] : %w[400 401 403 404 500 503])) : %w[401 403 404 422 500]
      required_errors.each do |status|
        r = resolve.call(responses[status])
        check(r.is_a?(Hash) && r.dig('content', 'application/json', 'schema', '$ref') == '#/components/schemas/ApiError', "error #{status} missing #{id}")
      end
      if op['x-vortex-collection']
        check(%w[cursor limit].all? { |n| params.any? { |p| p['in'] == 'query' && p['name'] == n } }, 'cursor/limit missing')
        page = resolve.call(responses.dig('200', 'content', 'application/json', 'schema'))
        check(page && page.fetch('required', []).include?('items') && page.fetch('properties', {}).key?('next_cursor'), 'page shape missing')
      end
      if responses.dig('200', 'content', 'text/event-stream')
        check(params.any? { |p| p['name'] == 'Last-Event-ID' && p['in'] == 'header' }, 'SSE resume header missing')
        sse = op.fetch('x-vortex-sse')
        check(sse['authority'] == 'PostgreSQL' && sse['gap_event'] == 'resync' && sse['gap_action'] == 'REST-refetch', 'SSE durability/resync missing')
        resolve.call({'$ref' => sse.fetch('event_schema')})
      end
    end
  end
  check(!ids.empty?, 'no operations')
  puts "OpenAPI lint passed: #{ids.length} business operations; explicit rotating refresh exception only"
rescue StandardError => e
  warn "OpenAPI lint failed: #{e.message}"
  exit 1
end
