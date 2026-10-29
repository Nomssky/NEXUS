import re

with open('PenpotPage.tsx', 'r') as fh:
    content = fh.read()

# Replace color: #XXXXXX → color: '#XXXXXX' (only when not already quoted)
content = re.sub(r"color:\s*#([0-9a-fA-F]{6})([^a-fA-F0-9'\"])", r"color: '#\1'\2", content)
# Fix background: #XXXXXX
content = re.sub(r"background:\s*#([0-9a-fA-F]{6})([^a-fA-F0-9'\"])", r"background: '#\1'\2", content)
# Fix backgroundColor: #XXXXXX
content = re.sub(r"backgroundColor:\s*#([0-9a-fA-F]{6})([^a-fA-F0-9'\"])", r"backgroundColor: '#\1'\2", content)

with open('PenpotPage.tsx', 'w') as fh:
    fh.write(content)
print('Fixed PenpotPage.tsx')
