"""Offline contract checks against real, locked provider SDKs."""
import asyncio
import os
from openai import OpenAI, APIStatusError as OpenAIStatusError
from anthropic import Anthropic, APIStatusError as AnthropicStatusError
from google import genai
from google.genai import types, errors
from pydantic import BaseModel, Field

url = os.environ["FAKEAPI_TEST_URL"]
key = os.environ["FAKEAPI_CLIENT_KEY"]
model = "fakeapi-test"

class Fixture(BaseModel):
    ok: bool
    source: str = Field(description='Origin of the fixture')

def run():
    with OpenAI(api_key=key, base_url=url + "/v1", max_retries=0) as client:
        assert len(client.models.list().data) == 2
        assert client.models.retrieve(model).id == model
        message = [{"role": "user", "content": "只回答 OK"}]
        result = client.chat.completions.create(model=model, messages=message)
        assert result.choices[0].message.content == "OK"
        with client.chat.completions.create(model=model, messages=message, stream=True, stream_options={"include_usage": True}) as stream:
            text = ""
            usage = None
            for event in stream:
                if event.choices:
                    text += event.choices[0].delta.content or ""
                if event.usage:
                    usage = event.usage
            assert text == "OK" and usage is not None
        result = client.responses.create(model=model, input="只回答 OK", store=False)
        assert result.output_text == "OK"
        with client.responses.stream(model=model, input="只回答 OK", store=False) as stream:
            text = "".join(event.delta for event in stream if event.type == 'response.output_text.delta')
            final = stream.get_final_response()
        assert text == "OK" and final.output_text == "OK"
        replay = [{"role": "user", "content": "old"}, *[item.model_dump(exclude_none=True) for item in final.output], {"role": "user", "content": "只回答 OK"}]
        assert client.responses.create(model=model, input=replay, store=False).output_text == "OK"
        parsed = client.responses.parse(model=model, input="/test/json", store=False, text_format=Fixture)
        assert parsed.output_parsed == Fixture(ok=True, source="fakeapi")
        tools = [{"type": "function", "name": "get_weather", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"], "additionalProperties": False}}]
        first = client.responses.create(model=model, input="/test/tool", tools=tools, tool_choice="required", store=False)
        call = first.output[0]
        assert call.type == "function_call" and call.name == "get_weather"
        result = client.responses.create(model=model, store=False, input=[{"role": "user", "content": "/test/tool"}, call.model_dump(exclude_none=True), {"type": "function_call_output", "call_id": call.call_id, "output": '{"temperature":25}'}])
        assert result.output_text == "工具测试完成。"
        try:
            client.responses.create(model=model, input='hello', store=True)
            raise AssertionError('stored Responses accepted')
        except OpenAIStatusError as e:
            assert e.status_code == 400
        with client.chat.completions.create(model='fakeapi-frog', messages=[{'role': 'user', 'content': 'SDK cancellation exercise'}], stream=True) as pending:
            next(pending)
        with client.responses.stream(model='fakeapi-frog', input='SDK cancellation exercise', store=False) as pending:
            next(iter(pending))
    print("PASS OpenAI Python: Chat, Responses, helpers, replay, JSON, tools, models, errors, close")
    with Anthropic(api_key=key, base_url=url + "/anthropic", max_retries=0) as client:
        assert len(client.models.list().data) == 2
        assert client.models.retrieve(model).id == model
        result = client.messages.create(model=model, max_tokens=128, messages=[{"role": "user", "content": "只回答 OK"}])
        assert result.content[0].text == "OK"
        with client.messages.stream(model=model, max_tokens=128, messages=[{"role": "user", "content": "只回答 OK"}]) as stream:
            text = "".join(stream.text_stream)
            final = stream.get_final_message()
        assert text == "OK" and final.content[0].text == "OK"
        tools = [{"name": "get_weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}]
        with client.messages.stream(model=model, max_tokens=128, messages=[{"role": "user", "content": "/test/tool"}], tools=tools, tool_choice={"type": "any"}) as stream:
            final = stream.get_final_message()
        call = final.content[0]
        assert call.type == "tool_use" and call.input == {"city": "Guangzhou"}
        result = client.messages.create(model=model, max_tokens=128, messages=[{"role": "user", "content": "/test/tool"}, {"role": "assistant", "content": [call.model_dump(exclude_none=True)]}, {"role": "user", "content": [{"type": "tool_result", "tool_use_id": call.id, "content": "25 C"}]}])
        assert result.content[0].text == "工具测试完成。"
        structured = client.messages.create(model=model, max_tokens=128, messages=[{'role':'user','content':'/test/json'}], output_config={'format':{'type':'json_schema','schema':Fixture.model_json_schema()}})
        assert Fixture.model_validate_json(structured.content[0].text) == Fixture(ok=True, source='fakeapi')
        try:
            client.messages.create(model=model, max_tokens=0, messages=[{'role': 'user', 'content': 'hello'}])
            raise AssertionError('invalid token limit accepted')
        except AnthropicStatusError as e:
            assert e.status_code == 400
        with client.messages.stream(model='fakeapi-frog', max_tokens=8192, messages=[{'role': 'user', 'content': 'SDK cancellation exercise'}]) as pending:
            next(pending.text_stream)
    print("PASS Anthropic Python: create, stream, final message, tool round trip, models, errors, close")
    client = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=url, api_version="v1beta", retry_options=types.HttpRetryOptions(attempts=1)))
    try:
        assert len(list(client.models.list())) == 2
        assert client.models.get(model=model).name == "models/" + model
        assert client.models.generate_content(model=model, contents="只回答 OK").text == "OK"
        text = "".join(event.text or "" for event in client.models.generate_content_stream(model=model, contents="只回答 OK"))
        assert text == "OK"
        result = client.models.generate_content(model=model, contents="/test/json", config=types.GenerateContentConfig(response_mime_type="application/json", response_schema=Fixture, automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True)))
        assert result.parsed == Fixture(ok=True, source="fakeapi")
        declaration = types.FunctionDeclaration(name='get_weather', parameters={'type': 'OBJECT', 'properties': {'city': {'type': 'STRING'}}, 'required': ['city']})
        result = client.models.generate_content(model=model, contents='/test/tool', config=types.GenerateContentConfig(tools=[types.Tool(function_declarations=[declaration])], automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True), tool_config=types.ToolConfig(function_calling_config=types.FunctionCallingConfig(mode='ANY'))))
        call = result.candidates[0].content.parts[0].function_call
        assert call.name == 'get_weather' and call.args == {'city': 'Guangzhou'}
        result = client.models.generate_content(model=model, contents=[types.Content(role='user', parts=[types.Part(text='/test/tool')]), types.Content(role='model', parts=[types.Part(function_call=call)]), types.Content(role='user', parts=[types.Part.from_function_response(name=call.name, response={'temperature': 25})])], config=types.GenerateContentConfig(automatic_function_calling=types.AutomaticFunctionCallingConfig(disable=True)))
        assert result.text == '工具测试完成。'
        chat = client.chats.create(model=model)
        assert chat.send_message("只回答 OK").text == "OK"
        try:
            client.models.generate_content(model='unknown-model', contents='hello')
            raise AssertionError('unknown model accepted')
        except errors.ClientError as e:
            assert e.code == 404
        pending = client.models.generate_content_stream(model='fakeapi-frog', contents='SDK cancellation exercise')
        next(pending)
        pending.close()
        assert chat.send_message("只回答 OK").text == "OK"
    finally:
        client.close()
    print("PASS Google Python: generate, stream, chat replay, Pydantic JSON, models, errors, close")
    asyncio.run(async_google())

async def async_google():
    client = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=url, api_version="v1beta"))
    try:
        stream = await client.aio.models.generate_content_stream(model=model, contents="只回答 OK")
        text = ""
        async for event in stream:
            text += event.text or ""
        assert text == "OK"
        pending = await client.aio.models.generate_content_stream(model='fakeapi-frog', contents='SDK cancellation exercise')
        await anext(pending)
        await pending.aclose()
    finally:
        await client.aio.aclose()
    print("PASS Google Python async stream")

if __name__ == "__main__":
    run()
