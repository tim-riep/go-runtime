<xsl:stylesheet version="3.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="text"/>
  <xsl:template match="/">gross:<xsl:value-of select="upper-case(/name)"/></xsl:template>
</xsl:stylesheet>
