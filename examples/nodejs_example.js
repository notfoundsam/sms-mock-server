/**
 * Example: Using Twilio Node.js SDK with SMS Mock Server
 *
 * Install dependencies:
 *     npm install twilio
 *
 * Usage:
 *     node examples/nodejs_example.js
 */

const twilio = require('twilio');

// Mock server configuration
const MOCK_SERVER_URL = 'http://localhost:8080';
const ACCOUNT_SID = 'ACXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX';
const AUTH_TOKEN = 'your_auth_token_here';

// Create Twilio client pointing to mock server
const client = twilio(ACCOUNT_SID, AUTH_TOKEN, {
    lazyLoading: true,
    accountSid: ACCOUNT_SID,
    apiBaseUrl: MOCK_SERVER_URL
});

async function sendSmsExample() {
    console.log('Sending SMS...');

    const message = await client.messages.create({
        to: '+15551234567',        // Registered number (will succeed)
        from: '+15550000001',      // Allowed From number
        body: 'Hello from mock server!',
        statusCallback: 'http://your-app.com/sms-callback'
    });

    console.log('✓ Message sent successfully!');
    console.log(`  SID: ${message.sid}`);
    console.log(`  Status: ${message.status}`);
    console.log(`  From: ${message.from}`);
    console.log(`  To: ${message.to}`);
    console.log(`  Body: ${message.body}`);
    console.log();
}

async function sendSmsToFailureNumber() {
    console.log('Sending SMS to failure number...');

    const message = await client.messages.create({
        to: '+15559999999',        // Failure number (will fail)
        from: '+15550000001',
        body: 'This message will fail',
        statusCallback: 'http://your-app.com/sms-callback'
    });

    console.log('✓ Message created (will fail during delivery)');
    console.log(`  SID: ${message.sid}`);
    console.log(`  Status: ${message.status}`);
    console.log();
}

async function makeCallExample() {
    console.log('Making call...');

    const call = await client.calls.create({
        to: '+15551234567',
        from: '+15550000001',
        url: 'http://your-twiml-server.com/voice',
        statusCallback: 'http://your-app.com/call-callback'
    });

    console.log('✓ Call initiated successfully!');
    console.log(`  SID: ${call.sid}`);
    console.log(`  Status: ${call.status}`);
    console.log(`  From: ${call.from}`);
    console.log(`  To: ${call.to}`);
    console.log();
}

async function testValidationErrors() {
    console.log('Testing validation errors...\n');

    // Test 1: Missing Body parameter
    console.log('1. Testing missing Body parameter...');
    try {
        await client.messages.create({
            to: '+15551234567',
            from: '+15550000001'
            // Missing Body
        });
    } catch (error) {
        console.log(`   ✓ Got expected error: ${error.message}\n`);
    }

    // Test 2: Invalid phone number format
    console.log('2. Testing invalid phone number format...');
    try {
        await client.messages.create({
            to: 'invalid-number',
            from: '+15550000001',
            body: 'Invalid number'
        });
    } catch (error) {
        console.log(`   ✓ Got expected error: ${error.message}\n`);
    }

    // Test 3: Invalid From number (not in allowed list)
    console.log('3. Testing invalid From number...');
    try {
        await client.messages.create({
            to: '+15551234567',
            from: '+15559999999',  // Not in allowed_from_numbers
            body: 'Invalid From number'
        });
    } catch (error) {
        console.log(`   ✓ Got expected error: ${error.message}\n`);
    }
}

async function main() {
    console.log('='.repeat(60));
    console.log('SMS Mock Server - Node.js SDK Example');
    console.log('='.repeat(60));
    console.log();

    try {
        // Run examples
        await sendSmsExample();
        await sendSmsToFailureNumber();
        await makeCallExample();
        await testValidationErrors();

        console.log('='.repeat(60));
        console.log('All examples completed!');
        console.log('Check the web UI at http://localhost:8080');
        console.log('='.repeat(60));
    } catch (error) {
        console.error('Error:', error.message);
    }
}

main();
