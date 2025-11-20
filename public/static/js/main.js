// Main JavaScript
console.log('Static JavaScript loaded successfully! 🚀');

document.addEventListener('DOMContentLoaded', function() {
    console.log('DOM fully loaded');
    
    // Add click event to button if it exists
    const button = document.querySelector('button');
    if (button) {
        button.addEventListener('click', function() {
            alert('Hello from static JavaScript file!');
            console.log('Button clicked!');
        });
    }
    
    // Display current time
    const timeElement = document.getElementById('current-time');
    if (timeElement) {
        const now = new Date();
        timeElement.textContent = now.toLocaleString();
    }
    
    // Animate success message
    const successElement = document.querySelector('.success');
    if (successElement) {
        setTimeout(() => {
            successElement.style.transform = 'scale(1.1)';
            setTimeout(() => {
                successElement.style.transform = 'scale(1)';
            }, 200);
        }, 500);
    }
});

// Test function
function testStaticJS() {
    return 'Static JS is working!';
}
